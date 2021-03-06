package tlsconfig

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// This is the currently active LetsEncrypt IdenTrust cross-signed CA cert.  It expires Mar 17, 2021.
const systemRootTrustedCert = `
-----BEGIN CERTIFICATE-----
MIIEkjCCA3qgAwIBAgIQCgFBQgAAAVOFc2oLheynCDANBgkqhkiG9w0BAQsFADA/
MSQwIgYDVQQKExtEaWdpdGFsIFNpZ25hdHVyZSBUcnVzdCBDby4xFzAVBgNVBAMT
DkRTVCBSb290IENBIFgzMB4XDTE2MDMxNzE2NDA0NloXDTIxMDMxNzE2NDA0Nlow
SjELMAkGA1UEBhMCVVMxFjAUBgNVBAoTDUxldCdzIEVuY3J5cHQxIzAhBgNVBAMT
GkxldCdzIEVuY3J5cHQgQXV0aG9yaXR5IFgzMIIBIjANBgkqhkiG9w0BAQEFAAOC
AQ8AMIIBCgKCAQEAnNMM8FrlLke3cl03g7NoYzDq1zUmGSXhvb418XCSL7e4S0EF
q6meNQhY7LEqxGiHC6PjdeTm86dicbp5gWAf15Gan/PQeGdxyGkOlZHP/uaZ6WA8
SMx+yk13EiSdRxta67nsHjcAHJyse6cF6s5K671B5TaYucv9bTyWaN8jKkKQDIZ0
Z8h/pZq4UmEUEz9l6YKHy9v6Dlb2honzhT+Xhq+w3Brvaw2VFn3EK6BlspkENnWA
a6xK8xuQSXgvopZPKiAlKQTGdMDQMc2PMTiVFrqoM7hD8bEfwzB/onkxEz0tNvjj
/PIzark5McWvxI0NHWQWM6r6hCm21AvA2H3DkwIDAQABo4IBfTCCAXkwEgYDVR0T
AQH/BAgwBgEB/wIBADAOBgNVHQ8BAf8EBAMCAYYwfwYIKwYBBQUHAQEEczBxMDIG
CCsGAQUFBzABhiZodHRwOi8vaXNyZy50cnVzdGlkLm9jc3AuaWRlbnRydXN0LmNv
bTA7BggrBgEFBQcwAoYvaHR0cDovL2FwcHMuaWRlbnRydXN0LmNvbS9yb290cy9k
c3Ryb290Y2F4My5wN2MwHwYDVR0jBBgwFoAUxKexpHsscfrb4UuQdf/EFWCFiRAw
VAYDVR0gBE0wSzAIBgZngQwBAgEwPwYLKwYBBAGC3xMBAQEwMDAuBggrBgEFBQcC
ARYiaHR0cDovL2Nwcy5yb290LXgxLmxldHNlbmNyeXB0Lm9yZzA8BgNVHR8ENTAz
MDGgL6AthitodHRwOi8vY3JsLmlkZW50cnVzdC5jb20vRFNUUk9PVENBWDNDUkwu
Y3JsMB0GA1UdDgQWBBSoSmpjBH3duubRObemRWXv86jsoTANBgkqhkiG9w0BAQsF
AAOCAQEA3TPXEfNjWDjdGBX7CVW+dla5cEilaUcne8IkCJLxWh9KEik3JHRRHGJo
uM2VcGfl96S8TihRzZvoroed6ti6WqEBmtzw3Wodatg+VyOeph4EYpr/1wXKtx8/
wApIvJSwtmVi4MFU5aMqrSDE6ea73Mj2tcMyo5jMd6jmeWUHK8so/joWUoHOUgwu
X4Po1QYz+3dszkDqMp4fklxBwXRsW10KXzPMTZ+sOPAveyxindmjkW8lGy+QsRlG
PfZ+G6Z6h7mjem0Y+iWlkYcV4PIWL1iwBi8saCbGS5jN2p8M+X+Q7UNKEkROb3N6
KOqkqm57TH2H3eDJAkSnh6/DNFu0Qg==
-----END CERTIFICATE-----
`

var certTemplate = x509.Certificate{
	SerialNumber: big.NewInt(199999),
	Subject: pkix.Name{
		CommonName: "test",
	},
	NotBefore: time.Now().AddDate(-1, 1, 1),
	NotAfter:  time.Now().AddDate(1, 1, 1),

	KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning, x509.ExtKeyUsageAny},

	BasicConstraintsValid: true,
}

func generateCertificate(t *testing.T, signer crypto.Signer, out io.Writer, isCA bool) {
	template := certTemplate
	template.IsCA = isCA
	if isCA {
		template.KeyUsage = template.KeyUsage | x509.KeyUsageCertSign
		template.MaxPathLen = 1
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &certTemplate, signer.Public(), signer)
	if err != nil {
		t.Fatal("Unable to generate a certificate", err.Error())
	}

	if err = pem.Encode(out, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		t.Fatal("Unable to write cert to file", err.Error())
	}
}

// generates a multiple-certificate CA file with both RSA and ECDSA certs and
// returns the filename so that cleanup can be deferred.
func generateMultiCert(t *testing.T, tempDir string) string {
	certOut, err := os.Create(filepath.Join(tempDir, "multi"))
	if err != nil {
		t.Fatal("Unable to create file to write multi-cert to", err.Error())
	}
	defer certOut.Close()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("Unable to generate RSA key for multi-cert", err.Error())
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("Unable to generate ECDSA key for multi-cert", err.Error())
	}

	for _, signer := range []crypto.Signer{rsaKey, ecKey} {
		generateCertificate(t, signer, certOut, true)
	}

	return certOut.Name()
}

func generateCertAndKey(t *testing.T, tempDir string) (string, string) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("Unable to generate RSA key", err.Error())

	}
	keyBytes := x509.MarshalPKCS1PrivateKey(rsaKey)

	keyOut, err := os.Create(filepath.Join(tempDir, "key"))
	if err != nil {
		t.Fatal("Unable to create file to write key to", err.Error())

	}
	defer keyOut.Close()

	if err = pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatal("Unable to write key to file", err.Error())
	}

	certOut, err := os.Create(filepath.Join(tempDir, "cert"))
	if err != nil {
		t.Fatal("Unable to create file to write cert to", err.Error())
	}
	defer certOut.Close()

	generateCertificate(t, rsaKey, certOut, false)

	return keyOut.Name(), certOut.Name()
}

func makeTempDir(t *testing.T) string {
	tempDir, err := ioutil.TempDir("", "tlsconfig-test")
	if err != nil {
		t.Fatal("Could not make a temporary directory", err.Error())
	}
	return tempDir
}

// If the cert files and directory are provided but are invalid, an error is
// returned.
func TestConfigServerTLSFailsIfUnableToLoadCerts(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)
	ca := generateMultiCert(t, tempDir)

	tempFile, err := ioutil.TempFile("", "cert-test")
	if err != nil {
		t.Fatal("Unable to create temporary empty file")
	}
	defer os.RemoveAll(tempFile.Name())
	tempFile.Close()

	for _, badFile := range []string{"not-a-file", tempFile.Name()} {
		for i := 0; i < 3; i++ {
			files := []string{cert, key, ca}
			files[i] = badFile

			result, err := Server(Options{
				CertFile:   files[0],
				KeyFile:    files[1],
				CAFile:     files[2],
				ClientAuth: tls.VerifyClientCertIfGiven,
			})
			if err == nil || result != nil {
				t.Fatal("Expected a non-real file to error and return a nil TLS config")
			}
		}
	}
}

// If server cert and key are provided and client auth and client CA are not
// set, a tls config with only the server certs will be returned.
func TestConfigServerTLSServerCertsOnly(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	keypair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal("Unable to load the generated cert and key")
	}

	tlsConfig, err := Server(Options{
		CertFile: cert,
		KeyFile:  key,
	})
	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure server TLS", err)
	}

	if len(tlsConfig.Certificates) != 1 {
		t.Fatal("Unexpected server certificates")
	}
	if len(tlsConfig.Certificates[0].Certificate) != len(keypair.Certificate) {
		t.Fatal("Unexpected server certificates")
	}
	for i, cert := range tlsConfig.Certificates[0].Certificate {
		if !bytes.Equal(cert, keypair.Certificate[i]) {
			t.Fatal("Unexpected server certificates")
		}
	}

	if !reflect.DeepEqual(tlsConfig.CipherSuites, DefaultServerAcceptedCiphers) {
		t.Fatal("Unexpected server cipher suites")
	}
	if !tlsConfig.PreferServerCipherSuites {
		t.Fatal("Expected server to prefer cipher suites")
	}
	if tlsConfig.MinVersion != tls.VersionTLS10 {
		t.Fatal("Unexpected server TLS version")
	}
}

// If client CA is provided, it will only be used if the client auth is >=
// VerifyClientCertIfGiven
func TestConfigServerTLSClientCANotSetIfClientAuthTooLow(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)
	ca := generateMultiCert(t, tempDir)

	tlsConfig, err := Server(Options{
		CertFile:   cert,
		KeyFile:    key,
		ClientAuth: tls.RequestClientCert,
		CAFile:     ca,
	})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure server TLS", err)
	}

	if len(tlsConfig.Certificates) != 1 {
		t.Fatal("Unexpected server certificates")
	}
	if tlsConfig.ClientAuth != tls.RequestClientCert {
		t.Fatal("ClientAuth was not set to what was in the options")
	}
	if tlsConfig.ClientCAs != nil {
		t.Fatalf("Client CAs should never have been set")
	}
}

// If client CA is provided, it will only be used if the client auth is >=
// VerifyClientCertIfGiven
func TestConfigServerTLSClientCASet(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)
	ca := generateMultiCert(t, tempDir)

	tlsConfig, err := Server(Options{
		CertFile:   cert,
		KeyFile:    key,
		ClientAuth: tls.VerifyClientCertIfGiven,
		CAFile:     ca,
	})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure server TLS", err)
	}

	if len(tlsConfig.Certificates) != 1 {
		t.Fatal("Unexpected server certificates")
	}
	if tlsConfig.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Fatal("ClientAuth was not set to what was in the options")
	}
	basePool, err := SystemCertPool()
	if err != nil {
		basePool = x509.NewCertPool()
	}
	// because we are not enabling `ExclusiveRootPools`, any root pool will also contain the system roots
	if tlsConfig.ClientCAs == nil || len(tlsConfig.ClientCAs.Subjects()) != len(basePool.Subjects())+2 {
		t.Fatalf("Client CAs were never set correctly")
	}
}

// Exclusive root pools determines whether the CA pool will be a union of the system
// certificate pool and custom certs, or an exclusive or of the custom certs and system pool
func TestConfigServerExclusiveRootPools(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)
	ca := generateMultiCert(t, tempDir)

	caBytes, err := ioutil.ReadFile(ca)
	if err != nil {
		t.Fatal("Unable to read CA certs", err)
	}

	var testCerts []*x509.Certificate
	for _, pemBytes := range [][]byte{caBytes, []byte(systemRootTrustedCert)} {
		pemBlock, _ := pem.Decode(pemBytes)
		if pemBlock == nil {
			t.Fatal("Malformed certificate")
		}
		cert, err := x509.ParseCertificate(pemBlock.Bytes)
		if err != nil {
			t.Fatal("Unable to parse certificate")
		}
		testCerts = append(testCerts, cert)
	}

	// ExclusiveRootPools not set, so should be able to verify both system-signed certs
	// and custom CA-signed certs
	tlsConfig, err := Server(Options{
		CertFile:   cert,
		KeyFile:    key,
		ClientAuth: tls.VerifyClientCertIfGiven,
		CAFile:     ca,
	})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure server TLS", err)
	}

	for i, cert := range testCerts {
		if _, err := cert.Verify(x509.VerifyOptions{Roots: tlsConfig.ClientCAs}); err != nil {
			t.Fatalf("Unable to verify certificate %d: %v", i, err)
		}
	}

	// ExclusiveRootPools set and custom CA provided, so system certs should not be verifiable
	// and custom CA-signed certs should be verifiable
	tlsConfig, err = Server(Options{
		CertFile:           cert,
		KeyFile:            key,
		ClientAuth:         tls.VerifyClientCertIfGiven,
		CAFile:             ca,
		ExclusiveRootPools: true,
	})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure server TLS", err)
	}

	for i, cert := range testCerts {
		_, err := cert.Verify(x509.VerifyOptions{Roots: tlsConfig.ClientCAs})
		switch {
		case i == 0 && err != nil:
			t.Fatal("Unable to verify custom certificate, even though the root pool should have only the custom CA", err)
		case i == 1 && err == nil:
			t.Fatal("Successfully verified system root-signed certificate though the root pool should have only the cusotm CA", err)
		}
	}

	// No CA file provided, system cert should be verifiable only
	tlsConfig, err = Server(Options{
		CertFile: cert,
		KeyFile:  key,
	})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure server TLS", err)
	}

	for i, cert := range testCerts {
		_, err := cert.Verify(x509.VerifyOptions{Roots: tlsConfig.ClientCAs})
		switch {
		case i == 1 && err != nil:
			t.Fatal("Unable to verify system root-signed certificate, even though the root pool should be the system pool only", err)
		case i == 0 && err == nil:
			t.Fatal("Successfully verified custom certificate though the root pool should be the system pool only", err)
		}
	}
}

// If a valid minimum version is specified in the options, the server's
// minimum version should be set accordingly
func TestConfigServerTLSMinVersionIsSetBasedOnOptions(t *testing.T) {
	versions := []uint16{
		tls.VersionTLS11,
		tls.VersionTLS12,
	}
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	for _, v := range versions {
		tlsConfig, err := Server(Options{
			MinVersion: v,
			CertFile:   cert,
			KeyFile:    key,
		})

		if err != nil || tlsConfig == nil {
			t.Fatal("Unable to configure server TLS", err)
		}

		if tlsConfig.MinVersion != v {
			t.Fatal("Unexpected minimum TLS version: ", tlsConfig.MinVersion)
		}
	}
}

// An error should be returned if the specified minimum version for the server
// is too low, i.e. less than VersionTLS10
func TestConfigServerTLSMinVersionNotSetIfMinVersionIsTooLow(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	_, err := Server(Options{
		MinVersion: tls.VersionSSL30,
		CertFile:   cert,
		KeyFile:    key,
	})

	if err == nil {
		t.Fatal("Should have returned an error for minimum version below TLS10")
	}
}

// An error should be returned if an invalid minimum version for the server is
// in the options struct
func TestConfigServerTLSMinVersionNotSetIfMinVersionIsInvalid(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	_, err := Server(Options{
		MinVersion: 1,
		CertFile:   cert,
		KeyFile:    key,
	})

	if err == nil {
		t.Fatal("Should have returned error on invalid minimum version option")
	}
}

// The root CA is never set if InsecureSkipBoolean is set to true, but the
// default client options are set
func TestConfigClientTLSNoVerify(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	ca := generateMultiCert(t, tempDir)

	tlsConfig, err := Client(Options{CAFile: ca, InsecureSkipVerify: true})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure client TLS", err)
	}

	if tlsConfig.RootCAs != nil {
		t.Fatal("Should not have set Root CAs", err)
	}

	if !reflect.DeepEqual(tlsConfig.CipherSuites, clientCipherSuites) {
		t.Fatal("Unexpected client cipher suites")
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("Unexpected client TLS version")
	}

	if tlsConfig.Certificates != nil {
		t.Fatal("Somehow client certificates were set")
	}
}

// The root CA is never set if InsecureSkipBoolean is set to false and root CA
// is not provided.
func TestConfigClientTLSNoRoot(t *testing.T) {
	tlsConfig, err := Client(Options{})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure client TLS", err)
	}

	if tlsConfig.RootCAs != nil {
		t.Fatal("Should not have set Root CAs", err)
	}

	if !reflect.DeepEqual(tlsConfig.CipherSuites, clientCipherSuites) {
		t.Fatal("Unexpected client cipher suites")
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("Unexpected client TLS version")
	}

	if tlsConfig.Certificates != nil {
		t.Fatal("Somehow client certificates were set")
	}
}

// The RootCA is set if the file is provided and InsecureSkipVerify is false
func TestConfigClientTLSRootCAFileWithOneCert(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	ca := generateMultiCert(t, tempDir)

	tlsConfig, err := Client(Options{CAFile: ca})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure client TLS", err)
	}
	basePool, err := SystemCertPool()
	if err != nil {
		basePool = x509.NewCertPool()
	}
	// because we are not enabling `ExclusiveRootPools`, any root pool will also contain the system roots
	if tlsConfig.RootCAs == nil || len(tlsConfig.RootCAs.Subjects()) != len(basePool.Subjects())+2 {
		t.Fatal("Root CAs not set properly", err)
	}
	if tlsConfig.Certificates != nil {
		t.Fatal("Somehow client certificates were set")
	}
}

// An error is returned if a root CA is provided but the file doesn't exist.
func TestConfigClientTLSNonexistentRootCAFile(t *testing.T) {
	tlsConfig, err := Client(Options{CAFile: "nonexistent"})

	if err == nil || tlsConfig != nil {
		t.Fatal("Should not have been able to configure client TLS", err)
	}
}

// An error is returned if either the client cert or the key are provided
// but invalid or blank.
func TestConfigClientTLSClientCertOrKeyInvalid(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	tempFile, err := ioutil.TempFile("", "cert-test")
	if err != nil {
		t.Fatal("Unable to create temporary empty file")
	}
	defer os.Remove(tempFile.Name())
	tempFile.Close()

	for i := 0; i < 2; i++ {
		for _, invalid := range []string{"not-a-file", "", tempFile.Name()} {
			files := []string{cert, key}
			files[i] = invalid

			tlsConfig, err := Client(Options{CertFile: files[0], KeyFile: files[1]})
			if err == nil || tlsConfig != nil {
				t.Fatal("Should not have been able to configure client TLS", err)
			}
		}
	}
}

// The certificate is set if the client cert and client key are provided and
// valid.
func TestConfigClientTLSValidClientCertAndKey(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	keypair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal("Unable to load the generated cert and key")
	}

	tlsConfig, err := Client(Options{CertFile: cert, KeyFile: key})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure client TLS", err)
	}

	if len(tlsConfig.Certificates) != 1 {
		t.Fatal("Unexpected client certificates")
	}
	if len(tlsConfig.Certificates[0].Certificate) != len(keypair.Certificate) {
		t.Fatal("Unexpected client certificates")
	}
	for i, cert := range tlsConfig.Certificates[0].Certificate {
		if !bytes.Equal(cert, keypair.Certificate[i]) {
			t.Fatal("Unexpected client certificates")
		}
	}

	if tlsConfig.RootCAs != nil {
		t.Fatal("Root CAs should not have been set", err)
	}
}

// Exclusive root pools determines whether the CA pool will be a union of the system
// certificate pool and custom certs, or an exclusive or of the custom certs and system pool
func TestConfigClientExclusiveRootPools(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	ca := generateMultiCert(t, tempDir)

	caBytes, err := ioutil.ReadFile(ca)
	if err != nil {
		t.Fatal("Unable to read CA certs", err)
	}

	var testCerts []*x509.Certificate
	for _, pemBytes := range [][]byte{caBytes, []byte(systemRootTrustedCert)} {
		pemBlock, _ := pem.Decode(pemBytes)
		if pemBlock == nil {
			t.Fatal("Malformed certificate")
		}
		cert, err := x509.ParseCertificate(pemBlock.Bytes)
		if err != nil {
			t.Fatal("Unable to parse certificate")
		}
		testCerts = append(testCerts, cert)
	}

	// ExclusiveRootPools not set, so should be able to verify both system-signed certs
	// and custom CA-signed certs
	tlsConfig, err := Client(Options{CAFile: ca})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure client TLS", err)
	}

	for i, cert := range testCerts {
		if _, err := cert.Verify(x509.VerifyOptions{Roots: tlsConfig.RootCAs}); err != nil {
			t.Fatalf("Unable to verify certificate %d: %v", i, err)
		}
	}

	// ExclusiveRootPools set and custom CA provided, so system certs should not be verifiable
	// and custom CA-signed certs should be verifiable
	tlsConfig, err = Client(Options{
		CAFile:             ca,
		ExclusiveRootPools: true,
	})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure client TLS", err)
	}

	for i, cert := range testCerts {
		_, err := cert.Verify(x509.VerifyOptions{Roots: tlsConfig.RootCAs})
		switch {
		case i == 0 && err != nil:
			t.Fatal("Unable to verify custom certificate, even though the root pool should have only the custom CA", err)
		case i == 1 && err == nil:
			t.Fatal("Successfully verified system root-signed certificate though the root pool should have only the cusotm CA", err)
		}
	}

	// No CA file provided, system cert should be verifiable only
	tlsConfig, err = Client(Options{})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure client TLS", err)
	}

	for i, cert := range testCerts {
		_, err := cert.Verify(x509.VerifyOptions{Roots: tlsConfig.RootCAs})
		switch {
		case i == 1 && err != nil:
			t.Fatal("Unable to verify system root-signed certificate, even though the root pool should be the system pool only", err)
		case i == 0 && err == nil:
			t.Fatal("Successfully verified custom certificate though the root pool should be the system pool only", err)
		}
	}
}

// If a valid MinVersion is specified in the options, the client's
// minimum version should be set accordingly
func TestConfigClientTLSMinVersionIsSetBasedOnOptions(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	tlsConfig, err := Client(Options{
		MinVersion: tls.VersionTLS12,
		CertFile:   cert,
		KeyFile:    key,
	})

	if err != nil || tlsConfig == nil {
		t.Fatal("Unable to configure client TLS", err)
	}

	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("Unexpected minimum TLS version: ", tlsConfig.MinVersion)
	}
}

// An error should be returned if the specified minimum version for the client
// is too low, i.e. less than VersionTLS12
func TestConfigClientTLSMinVersionNotSetIfMinVersionIsTooLow(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	_, err := Client(Options{
		MinVersion: tls.VersionTLS11,
		CertFile:   cert,
		KeyFile:    key,
	})

	if err == nil {
		t.Fatal("Should have returned an error for minimum version below TLS12")
	}
}

// An error should be returned if an invalid minimum version for the client is
// in the options struct
func TestConfigClientTLSMinVersionNotSetIfMinVersionIsInvalid(t *testing.T) {
	tempDir := makeTempDir(t)
	defer os.RemoveAll(tempDir)
	key, cert := generateCertAndKey(t, tempDir)

	_, err := Client(Options{
		MinVersion: 1,
		CertFile:   cert,
		KeyFile:    key,
	})

	if err == nil {
		t.Fatal("Should have returned error on invalid minimum version option")
	}
}
