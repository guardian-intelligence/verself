package deploydb

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type operatorConfig struct {
	Host            string
	Port            int
	CertificateFile string
	PrivateKeyFile  string
	CAConfig        string
}

type clickHouseClientConfigXML struct {
	XMLName xml.Name
	Secure  string `xml:"secure"`
	Host    string `xml:"host"`
	Port    int    `xml:"port"`
	OpenSSL struct {
		Client struct {
			CertificateFile string `xml:"certificateFile"`
			PrivateKeyFile  string `xml:"privateKeyFile"`
			CAConfig        string `xml:"caConfig"`
		} `xml:"client"`
	} `xml:"openSSL"`
}

func parseOperatorConfig(raw []byte) (operatorConfig, error) {
	var parsed clickHouseClientConfigXML
	if err := xml.Unmarshal(raw, &parsed); err != nil {
		return operatorConfig{}, fmt.Errorf("deploydb: parse operator config xml: %w", err)
	}
	cfg := operatorConfig{
		Host:            strings.TrimSpace(parsed.Host),
		Port:            parsed.Port,
		CertificateFile: strings.TrimSpace(parsed.OpenSSL.Client.CertificateFile),
		PrivateKeyFile:  strings.TrimSpace(parsed.OpenSSL.Client.PrivateKeyFile),
		CAConfig:        strings.TrimSpace(parsed.OpenSSL.Client.CAConfig),
	}
	secure, err := strconv.ParseBool(strings.TrimSpace(parsed.Secure))
	if err != nil {
		return operatorConfig{}, fmt.Errorf("deploydb: clickhouse operator secure flag must parse as bool: %w", err)
	}
	if !secure {
		return operatorConfig{}, fmt.Errorf("deploydb: clickhouse operator config must use TLS")
	}
	if cfg.Host == "" {
		return operatorConfig{}, fmt.Errorf("deploydb: clickhouse operator config missing host")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return operatorConfig{}, fmt.Errorf("deploydb: clickhouse operator config has invalid port %d", cfg.Port)
	}
	if cfg.CertificateFile == "" {
		return operatorConfig{}, fmt.Errorf("deploydb: clickhouse operator config missing certificateFile")
	}
	if cfg.PrivateKeyFile == "" {
		return operatorConfig{}, fmt.Errorf("deploydb: clickhouse operator config missing privateKeyFile")
	}
	if cfg.CAConfig == "" {
		return operatorConfig{}, fmt.Errorf("deploydb: clickhouse operator config missing caConfig")
	}
	return cfg, nil
}
