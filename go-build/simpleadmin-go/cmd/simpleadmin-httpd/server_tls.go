package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func ensureManagedTLSCertificate(certPath, keyPath, caCertPath, caKeyPath string) error {
	caCert, caKey, err := ensureLocalCACertificate(caCertPath, caKeyPath)
	if err != nil {
		return err
	}
	if serverCertificateMatches(certPath, keyPath, caCert) {
		return nil
	}
	if cert, ok := loadUsableUserCertificate(certPath, keyPath, caCert); ok {
		log.Printf("HTTPS 使用用户自带证书 %s (subject: %s),跳过本地托管证书覆写;如需恢复托管证书请删除该文件", certPath, cert.Subject.String())
		return nil
	}
	log.Printf("HTTPS 证书缺失、损坏、过期或与私钥不匹配,生成本地自签证书: %s", certPath)
	return writeServerCertificate(certPath, keyPath, caCert, caKey)
}

// loadUsableUserCertificate reports whether certPath/keyPath hold a usable
// user-provided certificate: parseable, within its validity window and
// matching the private key, regardless of issuer. Certificates issued by the
// local CA are left to the managed renewal path instead.
func loadUsableUserCertificate(certPath, keyPath string, caCert *x509.Certificate) (*x509.Certificate, bool) {
	cert, err := loadSingleCertificate(certPath)
	if err != nil {
		return nil, false
	}
	if cert.IsCA {
		return nil, false
	}
	if err := cert.CheckSignatureFrom(caCert); err == nil {
		return nil, false
	}
	// 本地 CA 被重新生成后(过期/损坏/丢密钥),由旧 CA 签发的托管证书无法通过
	// 对新 CA 的 CheckSignatureFrom,会被误判成"用户自带证书"而永久不再续期。
	// 用主题/颁发者标记识别托管证书,交回托管重签路径。
	if isManagedCertificate(cert) {
		return nil, false
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return nil, false
	}
	key, err := loadPrivateKey(keyPath)
	if err != nil {
		return nil, false
	}
	if !publicKeyMatches(cert.PublicKey, key.Public()) {
		return nil, false
	}
	return cert, true
}

// isManagedCertificate 依据主题/颁发者标记识别本服务托管模板签发的证书,
// 不依赖当前 CA 的签名:CA 重新生成后,旧 CA 签发的托管证书无法通过
// CheckSignatureFrom,若仅凭签名区分就会被误判为用户证书而跳过续期。
// 标记取自 writeServerCertificate / ensureLocalCACertificate 的固定模板。
func isManagedCertificate(cert *x509.Certificate) bool {
	if cert.IsCA {
		return false
	}
	return cert.Subject.CommonName == "ZBIMS Local HTTPS" &&
		cert.Issuer.CommonName == "ZBIMS Local Root CA"
}

func publicKeyMatches(a, b crypto.PublicKey) bool {
	equal, ok := a.(interface{ Equal(crypto.PublicKey) bool })
	if !ok {
		return false
	}
	return equal.Equal(b)
}

// loadPrivateKey parses an RSA (PKCS#1), PKCS#8 or EC private key PEM file so
// user-provided certificates are not rejected for their key encoding.
func loadPrivateKey(path string) (crypto.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("missing private key PEM block")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, errors.New("private key does not implement crypto.Signer")
		}
		return signer, nil
	default:
		return nil, fmt.Errorf("unsupported private key PEM type: %s", block.Type)
	}
}

func ensureLocalCACertificate(certPath, keyPath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	if cert, key, err := loadLocalCACertificate(certPath, keyPath); err == nil {
		return cert, key, nil
	}

	if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return nil, nil, err
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	serialNumber, err := randomSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"ZBIMS Local CA"},
			CommonName:   "ZBIMS Local Root CA",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}
	if err := writeCertificatePEM(certPath, certDER); err != nil {
		return nil, nil, err
	}
	if err := writeRSAPrivateKeyPEM(keyPath, privateKey); err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, err
	}
	return cert, privateKey, nil
}

func loadLocalCACertificate(certPath, keyPath string) (*x509.Certificate, *rsa.PrivateKey, error) {
	cert, err := loadSingleCertificate(certPath)
	if err != nil {
		return nil, nil, err
	}
	key, err := loadRSAPrivateKey(keyPath)
	if err != nil {
		return nil, nil, err
	}
	// CA 证书与 CA 私钥不配对(设备迁移/部分备份还原的遗留)时,用其签发的
	// 服务器证书必然无法通过客户端链校验,整个 HTTPS 不可用;判为不可用,
	// 交由 ensureLocalCACertificate 重新生成配对。
	if !publicKeyMatches(cert.PublicKey, key.Public()) {
		return nil, nil, errors.New("local CA certificate does not match its private key")
	}
	if !cert.IsCA || time.Now().After(cert.NotAfter.AddDate(0, 0, -30)) {
		return nil, nil, errors.New("local CA certificate is invalid or expiring")
	}
	return cert, key, nil
}

func serverCertificateMatches(certPath, keyPath string, caCert *x509.Certificate) bool {
	cert, err := loadSingleCertificate(certPath)
	if err != nil {
		return false
	}
	key, err := loadRSAPrivateKey(keyPath)
	if err != nil {
		return false
	}
	// 证书与私钥各自可解析不代表配对:不匹配时 ListenAndServeTLS 会以
	// "private key does not match public key" 直接失败并 log.Fatal 终止进程,
	// 必须在此检出并走重新生成的自愈路径(如写证书中途断电留下新证书+旧私钥)。
	if !publicKeyMatches(cert.PublicKey, key.Public()) {
		return false
	}
	if cert.IsCA || time.Now().After(cert.NotAfter.AddDate(0, 0, -30)) {
		return false
	}
	if err := cert.CheckSignatureFrom(caCert); err != nil {
		return false
	}
	if !hasExtKeyUsage(cert, x509.ExtKeyUsageServerAuth) {
		return false
	}
	for _, ip := range requiredTLSCertificateIPs() {
		if !certificateHasIP(cert, ip) {
			return false
		}
	}
	return true
}

func writeServerCertificate(certPath, keyPath string, caCert *x509.Certificate, caKey *rsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return err
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serialNumber, err := randomSerialNumber()
	if err != nil {
		return err
	}

	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"ZBIMS"},
			CommonName:   "ZBIMS Local HTTPS",
		},
		DNSNames:              requiredTLSCertificateDNSNames(),
		IPAddresses:           requiredTLSCertificateIPs(),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, &privateKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writeCertificatePEM(certPath, certDER); err != nil {
		return err
	}
	return writeRSAPrivateKeyPEM(keyPath, privateKey)
}

func requiredTLSCertificateDNSNames() []string {
	return []string{"localhost", "zbims", "zbims.local"}
}

func requiredTLSCertificateIPs() []net.IP {
	seen := map[string]bool{}
	ips := make([]net.IP, 0, 8)
	addIP := func(value string) {
		ip := net.ParseIP(value)
		if ip == nil {
			return
		}
		key := ip.String()
		if seen[key] {
			return
		}
		seen[key] = true
		ips = append(ips, ip)
	}

	addIP("127.0.0.1")
	addIP("::1")
	addIP("192.168.225.1")

	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			addIP(ip.String())
		}
	}
	return ips
}

func randomSerialNumber() (*big.Int, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, serialLimit)
}

func hasExtKeyUsage(cert *x509.Certificate, usage x509.ExtKeyUsage) bool {
	for _, item := range cert.ExtKeyUsage {
		if item == usage {
			return true
		}
	}
	return false
}

func certificateHasIP(cert *x509.Certificate, want net.IP) bool {
	for _, got := range cert.IPAddresses {
		if got.Equal(want) {
			return true
		}
	}
	return false
}

func loadSingleCertificate(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("missing certificate PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func loadRSAPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, errors.New("missing RSA private key PEM block")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func writeCertificatePEM(path string, certDER []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if err := pem.Encode(file, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeRSAPrivateKeyPEM(path string, privateKey *rsa.PrivateKey) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	keyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	if err := pem.Encode(file, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
