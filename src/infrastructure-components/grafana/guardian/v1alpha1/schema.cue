package v1alpha1

#NonEmptyString: string & !=""
#AbsolutePath:   =~"^/[^\\s]*$"
#RepoPath:       string & !~"^/" & !~"(^|/)\\.\\.(/|$)" & !=""
#Host:           #NonEmptyString
#HTTPURL:        =~"^https?://[^\\s]+$"
#DNSName:        =~"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$"

#ObjectRef: {
	apiVersion: #NonEmptyString
	kind:       #NonEmptyString
	name:       #NonEmptyString
}

#Metadata: {
	name: #NonEmptyString
}

#GrafanaInstance: {
	apiVersion: "grafana.guardianintelligence.org/v1alpha1"
	kind:       "GrafanaInstance"
	metadata:   #Metadata
	spec: {
		runtimeArtifact: #RepoPath
		runtimeRoot:     #AbsolutePath
		configPath:      #AbsolutePath
		dataDir:         #AbsolutePath
		logDir:          #AbsolutePath
		reportPath:      #AbsolutePath
		user:            #NonEmptyString
		group:           #NonEmptyString
		server: {
			httpAddr: #Host
			httpPort: int & >=1 & <=65535
			domain:   #DNSName
			rootURL:  #HTTPURL
		}
		database: {
			host:    #AbsolutePath
			name:    #NonEmptyString
			user:    #NonEmptyString
			sslMode: "disable" | "require" | "verify-ca" | "verify-full"
		}
		openBao: {
			address: #HTTPURL
			caCert:  #AbsolutePath
			adminPasswordRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
			secretKeyRef: #ObjectRef & {
				apiVersion: "openbao.guardianintelligence.org/v1alpha1"
				kind:       "SecretPath"
			}
		}
		authJWT: {
			enabled:      bool
			headerName:   #NonEmptyString
			emailClaim:   #NonEmptyString
			usernameClaim: #NonEmptyString
			jwkSetURL:    #HTTPURL
			cacheTTL:     #NonEmptyString
			autoSignUp:   bool
		}
	}
}
