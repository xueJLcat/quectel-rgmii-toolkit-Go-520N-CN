package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsureAuthFileRepairsCorruptFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"no colon line", "garbage content without colon\n"},
		{"comments only", "# only a comment\n\n"},
	}
	for _, check := range cases {
		t.Run(check.name, func(t *testing.T) {
			dir := t.TempDir()
			authFile := filepath.Join(dir, "simpleadmin.auth")
			if err := os.WriteFile(authFile, []byte(check.content), 0600); err != nil {
				t.Fatalf("write corrupt auth file: %v", err)
			}

			if err := ensureAuthFile(authFile); err != nil {
				t.Fatalf("ensureAuthFile: %v", err)
			}

			auth, err := loadAuthConfig(authFile)
			if err != nil {
				t.Fatalf("loadAuthConfig after repair: %v", err)
			}
			if auth.Username != "admin" || auth.Password != "admin" {
				t.Fatalf("auth = %#v, want default admin/admin", auth)
			}

			backups, err := filepath.Glob(authFile + ".corrupt-*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("backup files = %v (err %v), want exactly one", backups, err)
			}
			data, err := os.ReadFile(backups[0])
			if err != nil {
				t.Fatalf("read backup: %v", err)
			}
			if string(data) != check.content {
				t.Fatalf("backup content = %q, want original %q", data, check.content)
			}
		})
	}
}

func TestEnsureAuthFileValidFileLeavesNoBackup(t *testing.T) {
	dir := t.TempDir()
	authFile := filepath.Join(dir, "simpleadmin.auth")
	if err := os.WriteFile(authFile, []byte("admin:keepme\n"), 0600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	if err := ensureAuthFile(authFile); err != nil {
		t.Fatalf("ensureAuthFile: %v", err)
	}
	backups, err := filepath.Glob(authFile + ".corrupt-*")
	if err != nil || len(backups) != 0 {
		t.Fatalf("backup files = %v (err %v), want none", backups, err)
	}
}

func generateUserCertificateForTest(t *testing.T, notBefore, notAfter time.Time) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate user key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "user-provided.example.com"},
		DNSNames:              []string{"user-provided.example.com"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create user certificate: %v", err)
	}
	return der, key
}

func writeUserCertificateForTest(t *testing.T, dir string, notBefore, notAfter time.Time) (certPath, keyPath, caCertPath, caKeyPath string) {
	t.Helper()
	certPath = filepath.Join(dir, "server.crt")
	keyPath = filepath.Join(dir, "server.key")
	caCertPath = filepath.Join(dir, "zbims-ca.crt")
	caKeyPath = filepath.Join(dir, "zbims-ca.key")
	der, key := generateUserCertificateForTest(t, notBefore, notAfter)
	if err := writeCertificatePEM(certPath, der); err != nil {
		t.Fatalf("write user certificate: %v", err)
	}
	if err := writeRSAPrivateKeyPEM(keyPath, key); err != nil {
		t.Fatalf("write user key: %v", err)
	}
	return certPath, keyPath, caCertPath, caKeyPath
}

func assertManagedCertificate(t *testing.T, certPath, keyPath, caCertPath, caKeyPath string) {
	t.Helper()
	cert, err := loadSingleCertificate(certPath)
	if err != nil {
		t.Fatalf("load generated certificate: %v", err)
	}
	key, err := loadRSAPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("load generated key: %v", err)
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		t.Fatalf("generated certificate does not match generated key")
	}
	caCert, _, err := loadLocalCACertificate(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("load local CA: %v", err)
	}
	if err := cert.CheckSignatureFrom(caCert); err != nil {
		t.Fatalf("generated certificate is not signed by the local CA: %v", err)
	}
}

func TestEnsureManagedTLSCertificateKeepsUserCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, caCertPath, caKeyPath := writeUserCertificateForTest(t, dir, time.Now().Add(-time.Hour), time.Now().AddDate(1, 0, 0))
	beforeCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read user certificate: %v", err)
	}
	beforeKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read user key: %v", err)
	}

	if err := ensureManagedTLSCertificate(certPath, keyPath, caCertPath, caKeyPath); err != nil {
		t.Fatalf("ensureManagedTLSCertificate: %v", err)
	}

	afterCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read certificate after ensure: %v", err)
	}
	afterKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key after ensure: %v", err)
	}
	if !bytes.Equal(beforeCert, afterCert) {
		t.Fatalf("user-provided certificate was overwritten")
	}
	if !bytes.Equal(beforeKey, afterKey) {
		t.Fatalf("user-provided private key was overwritten")
	}
}

func TestEnsureManagedTLSCertificateGeneratesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	caCertPath := filepath.Join(dir, "zbims-ca.crt")
	caKeyPath := filepath.Join(dir, "zbims-ca.key")

	if err := ensureManagedTLSCertificate(certPath, keyPath, caCertPath, caKeyPath); err != nil {
		t.Fatalf("ensureManagedTLSCertificate: %v", err)
	}
	assertManagedCertificate(t, certPath, keyPath, caCertPath, caKeyPath)
}

func TestEnsureManagedTLSCertificateRegeneratesCorruptCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	caCertPath := filepath.Join(dir, "zbims-ca.crt")
	caKeyPath := filepath.Join(dir, "zbims-ca.key")
	if err := ensureManagedTLSCertificate(certPath, keyPath, caCertPath, caKeyPath); err != nil {
		t.Fatalf("seed managed certificate: %v", err)
	}

	corrupt := []byte("not a certificate\n")
	if err := os.WriteFile(certPath, corrupt, 0644); err != nil {
		t.Fatalf("corrupt certificate: %v", err)
	}
	if err := ensureManagedTLSCertificate(certPath, keyPath, caCertPath, caKeyPath); err != nil {
		t.Fatalf("ensureManagedTLSCertificate: %v", err)
	}

	afterCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read certificate after repair: %v", err)
	}
	if bytes.Equal(afterCert, corrupt) {
		t.Fatalf("corrupt certificate was left in place")
	}
	assertManagedCertificate(t, certPath, keyPath, caCertPath, caKeyPath)
}

func TestEnsureManagedTLSCertificateReplacesExpiredUserCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath, caCertPath, caKeyPath := writeUserCertificateForTest(t, dir, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	beforeCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read user certificate: %v", err)
	}

	if err := ensureManagedTLSCertificate(certPath, keyPath, caCertPath, caKeyPath); err != nil {
		t.Fatalf("ensureManagedTLSCertificate: %v", err)
	}

	afterCert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read certificate after ensure: %v", err)
	}
	if bytes.Equal(beforeCert, afterCert) {
		t.Fatalf("expired user certificate was kept instead of replaced")
	}
	assertManagedCertificate(t, certPath, keyPath, caCertPath, caKeyPath)
}

func TestEnsureManagedTLSCertificateReplacesMismatchedKey(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	caCertPath := filepath.Join(dir, "zbims-ca.crt")
	caKeyPath := filepath.Join(dir, "zbims-ca.key")

	certDER, _ := generateUserCertificateForTest(t, time.Now().Add(-time.Hour), time.Now().AddDate(1, 0, 0))
	_, foreignKey := generateUserCertificateForTest(t, time.Now().Add(-time.Hour), time.Now().AddDate(1, 0, 0))
	if err := writeCertificatePEM(certPath, certDER); err != nil {
		t.Fatalf("write user certificate: %v", err)
	}
	if err := writeRSAPrivateKeyPEM(keyPath, foreignKey); err != nil {
		t.Fatalf("write mismatched key: %v", err)
	}

	if err := ensureManagedTLSCertificate(certPath, keyPath, caCertPath, caKeyPath); err != nil {
		t.Fatalf("ensureManagedTLSCertificate: %v", err)
	}
	assertManagedCertificate(t, certPath, keyPath, caCertPath, caKeyPath)
}

func TestEnsureManagedTLSCertificateStillRenewsExpiringManagedCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	caCertPath := filepath.Join(dir, "zbims-ca.crt")
	caKeyPath := filepath.Join(dir, "zbims-ca.key")

	caCert, caKey, err := ensureLocalCACertificate(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("ensure local CA: %v", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate serial: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"ZBIMS"}, CommonName: "ZBIMS Local HTTPS"},
		DNSNames:              requiredTLSCertificateDNSNames(),
		IPAddresses:           requiredTLSCertificateIPs(),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, 1),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create expiring managed certificate: %v", err)
	}
	if err := writeCertificatePEM(certPath, der); err != nil {
		t.Fatalf("write expiring certificate: %v", err)
	}
	if err := writeRSAPrivateKeyPEM(keyPath, key); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if serverCertificateMatches(certPath, keyPath, caCert) {
		t.Fatalf("expiring managed certificate unexpectedly reported as matching")
	}

	if err := ensureManagedTLSCertificate(certPath, keyPath, caCertPath, caKeyPath); err != nil {
		t.Fatalf("ensureManagedTLSCertificate: %v", err)
	}
	renewed, err := loadSingleCertificate(certPath)
	if err != nil {
		t.Fatalf("load renewed certificate: %v", err)
	}
	if time.Now().After(renewed.NotAfter.AddDate(0, 0, -30)) {
		t.Fatalf("managed certificate inside the renewal window was not renewed (NotAfter %s)", renewed.NotAfter)
	}
	assertManagedCertificate(t, certPath, keyPath, caCertPath, caKeyPath)
}

func TestHTTPSRedirectLocationPreservesPreviousBehaviour(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://router.local/index.html?next=1", nil)
	req.Host = "router.local:80"

	checks := []struct {
		name      string
		httpsAddr string
		want      string
	}{
		{"non default port", ":8443", "https://router.local:8443/index.html?next=1"},
		{"explicit host and port", "192.168.1.1:8443", "https://router.local:8443/index.html?next=1"},
		{"default port", ":443", "https://router.local/index.html?next=1"},
		{"empty addr", "", "https://router.local/index.html?next=1"},
	}
	for _, check := range checks {
		if got := httpsRedirectLocation(check.httpsAddr, req); got != check.want {
			t.Fatalf("%s: httpsRedirectLocation(%q) = %q, want %q", check.name, check.httpsAddr, got, check.want)
		}
	}

	noPortReq := httptest.NewRequest(http.MethodGet, "http://router.local/", nil)
	if got, want := httpsRedirectLocation(":8443", noPortReq), "https://router.local:8443/"; got != want {
		t.Fatalf("host without port: location = %q, want %q", got, want)
	}
}

func TestExplicitHTTPSFlagDetectedForWarning(t *testing.T) {
	cfg := serverConfig{}
	fs := newServeFlagSet(&cfg)
	if err := fs.Parse([]string{"-https", ":8443"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !httpsAddrExplicitlySet(fs) {
		t.Fatalf("-https was not detected as explicitly set")
	}
	if !cfg.noTLS {
		t.Fatalf("no-tls default changed: %v", cfg.noTLS)
	}

	cfg2 := serverConfig{}
	fs2 := newServeFlagSet(&cfg2)
	if err := fs2.Parse(nil); err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if httpsAddrExplicitlySet(fs2) {
		t.Fatalf("default invocation must not count as explicit -https")
	}

	cfg3 := serverConfig{}
	fs3 := newServeFlagSet(&cfg3)
	if err := fs3.Parse([]string{"-https", ":8443", "-no-tls=false"}); err != nil {
		t.Fatalf("parse with -no-tls=false: %v", err)
	}
	if cfg3.noTLS && httpsAddrExplicitlySet(fs3) {
		t.Fatalf("warning condition must be false once HTTPS is enabled")
	}
}
