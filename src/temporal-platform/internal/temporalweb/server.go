package temporalweb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"github.com/temporalio/ui-server/v2/plugins/fs_config_provider"
	uiapi "github.com/temporalio/ui-server/v2/server/api"
	"github.com/temporalio/ui-server/v2/server/auth"
	uiconfig "github.com/temporalio/ui-server/v2/server/config"
	"github.com/temporalio/ui-server/v2/server/cors"
	"github.com/temporalio/ui-server/v2/server/csrf"
	"github.com/temporalio/ui-server/v2/server/headers"
	"github.com/temporalio/ui-server/v2/server/route"
	"github.com/temporalio/ui-server/v2/ui"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Config struct {
	ConfigDir        string
	Environment      string
	FrontendAddress  string
	SPIFFESocketAddr string
}

func Run(ctx context.Context, cfg Config) error {
	cfgProvider := fs_config_provider.NewFSConfigProvider(strings.TrimSpace(cfg.ConfigDir), strings.TrimSpace(cfg.Environment))
	refreshedProvider, err := uiconfig.NewConfigProviderWithRefresh(cfgProvider)
	if err != nil {
		return fmt.Errorf("create temporal web config provider: %w", err)
	}

	serverCfg, err := refreshedProvider.GetConfig()
	if err != nil {
		return fmt.Errorf("load temporal web config: %w", err)
	}
	if err := serverCfg.Validate(); err != nil {
		return fmt.Errorf("validate temporal web config: %w", err)
	}

	source, err := workloadauth.Source(ctx, strings.TrimSpace(cfg.SPIFFESocketAddr))
	if err != nil {
		return fmt.Errorf("open SPIFFE X509 source: %w", err)
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			slog.ErrorContext(context.Background(), "close temporal web spiffe source", "error", closeErr)
		}
	}()

	if _, err := workloadauth.CurrentIDForService(source, workloadauth.ServiceTemporalWeb); err != nil {
		return err
	}

	grpcConn, err := dialTemporalFrontend(ctx, strings.TrimSpace(cfg.FrontendAddress), source)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := grpcConn.Close(); closeErr != nil {
			slog.ErrorContext(context.Background(), "close temporal web grpc conn", "error", closeErr)
		}
	}()

	echoServer, err := buildHTTPServer(serverCfg, refreshedProvider, grpcConn)
	if err != nil {
		return err
	}

	listenAddr := net.JoinHostPort(strings.TrimSpace(serverCfg.Host), strconv.Itoa(serverCfg.Port))
	httpServer := &http.Server{
		Addr:              listenAddr,
		Handler:           otelhttp.NewHandler(echoServer, "temporal-web"),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if serverCfg.UIServerTLS.CertFile != "" && serverCfg.UIServerTLS.KeyFile != "" {
			serverErrCh <- httpServer.ListenAndServeTLS(serverCfg.UIServerTLS.CertFile, serverCfg.UIServerTLS.KeyFile)
			return
		}
		serverErrCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve temporal web: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown temporal web: %w", err)
		}
		err := <-serverErrCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("serve temporal web after shutdown: %w", err)
		}
		return nil
	}
}

func dialTemporalFrontend(
	ctx context.Context,
	address string,
	source *workloadapi.X509Source,
) (*grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	serverID, err := workloadauth.PeerIDForSource(source, workloadauth.ServiceTemporalServer)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.DialContext(
		dialCtx,
		address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(serverID)))),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("dial temporal frontend %s: %w", address, err)
	}
	return conn, nil
}

func buildHTTPServer(
	cfg *uiconfig.Config,
	cfgProvider *uiconfig.ConfigProviderWithRefresh,
	grpcConn *grpc.ClientConn,
) (*echo.Echo, error) {
	e := echo.New()
	e.HideBanner = cfg.HideLogs
	e.HidePort = cfg.HideLogs

	if !cfg.HideLogs {
		e.Use(middleware.Logger())
	}
	e.Use(middleware.Recover())
	e.Use(middleware.Gzip())
	e.Use(cors.CORSMiddleware(cors.CORSConfig{
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			echo.HeaderXCSRFToken,
			echo.HeaderAuthorization,
			auth.AuthorizationExtrasHeader,
			"Caller-Type",
		},
		AllowCredentials: true,
		ConfigProvider:   cfgProvider,
	}))
	e.Use(middleware.Secure())
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		CookiePath:     "/",
		CookieHTTPOnly: false,
		CookieSameSite: http.SameSiteStrictMode,
		CookieSecure:   !cfg.CORS.CookieInsecure,
		Skipper:        csrf.SkipOnAuthorizationHeader,
	}))

	e.Pre(route.PublicPath(cfg.PublicPath))
	route.SetHealthRoute(e)
	setAPIRoutes(e, cfgProvider, grpcConn, buildAPIMiddleware(cfg))
	route.SetAuthRoutes(e, cfgProvider)

	if cfg.EnableUI {
		assets, err := resolveUIAssets(cfg)
		if err != nil {
			return nil, err
		}
		if err := route.SetUIRoutes(e, cfg.PublicPath, assets); err != nil {
			return nil, fmt.Errorf("set temporal web ui routes: %w", err)
		}
		route.SetRenderRoute(e, cfg.PublicPath)
	}

	return e, nil
}

func buildAPIMiddleware(cfg *uiconfig.Config) []uiapi.Middleware {
	headersToForward := uniqueStrings(append([]string{
		auth.AuthorizationExtrasHeader,
		"Caller-Type",
	}, cfg.ForwardHeaders...))
	return []uiapi.Middleware{
		headers.WithForwardHeaders(headersToForward),
	}
}

func resolveUIAssets(cfg *uiconfig.Config) (fs.FS, error) {
	if strings.TrimSpace(cfg.UIAssetPath) != "" {
		return os.DirFS(strings.TrimSpace(cfg.UIAssetPath)), nil
	}

	assets, err := ui.Assets()
	if err != nil {
		return nil, fmt.Errorf("load embedded temporal web assets: %w", err)
	}

	assetDir := "local"
	if cfg.CloudUI {
		assetDir = "cloud"
	}
	assets, err = fs.Sub(assets, assetDir)
	if err != nil {
		return nil, fmt.Errorf("load temporal web asset dir %s: %w", assetDir, err)
	}
	return assets, nil
}

func setAPIRoutes(
	e *echo.Echo,
	cfgProvider *uiconfig.ConfigProviderWithRefresh,
	grpcConn *grpc.ClientConn,
	apiMiddleware []uiapi.Middleware,
) {
	group := e.Group("/api/v1")
	group.GET("/settings", uiapi.GetSettings(cfgProvider))

	writeControl := route.DisableWriteMiddleware(cfgProvider)
	group.GET(
		uiapi.WorkflowRawHistoryUrl,
		uiapi.WorkflowRawHistoryHandler(workflowservice.NewWorkflowServiceClient(grpcConn)),
		writeControl,
	)
	group.Match(
		[]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete},
		"/*",
		uiapi.TemporalAPIHandler(cfgProvider, apiMiddleware, grpcConn),
		writeControl,
	)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
