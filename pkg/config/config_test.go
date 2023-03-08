package proxyconfig

import (
	"os"
	"testing"
)

func TestConfigFromFile(t *testing.T) {
	file, err := os.CreateTemp(os.TempDir(), "")
	if err != nil {
		t.Errorf("Could not create temp file:")
	}

	fileContents := `
tls_server_config:
  cert_file: "server.crt"
  key_file : "server.key"
  client_auth_type : "VerifyClientCertIfGiven"
  client_ca_file : "tls-ca-chain.pem"
`
	file.Write([]byte(fileContents))
	configFilePath := file.Name()

	cfg, err := ConfigFromFile(configFilePath)
	if err != nil {
		t.Errorf("Error was not nil: %+v", err)
	}

	if cfg.WebConfig.TLSCertPath != "server.crt" {
		t.Errorf("Invalid TLSKeypath. Expected 'server.crt', Got '%s'", cfg.WebConfig.TLSCertPath)
	}
	if cfg.WebConfig.TLSKeyPath != "server.key" {
		t.Errorf("Invalid TLSCertPath. Expected 'server.key', Got '%s'", cfg.WebConfig.TLSKeyPath)
	}
	if cfg.WebConfig.ClientAuth != "VerifyClientCertIfGiven" {
		t.Errorf("Invalid ClientAuth. Expected 'VerifyClientCertIfGiven', Got '%s'", cfg.WebConfig.ClientAuth)
	}
	if cfg.WebConfig.ClientCAs != "tls-ca-chain.pem" {
		t.Errorf("Invalid ClientCAs. Expected 'tls-ca-chain.pem', Got '%s'", cfg.WebConfig.ClientCAs)
	}
}
