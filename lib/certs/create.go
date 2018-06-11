package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"strings"
	"time"

	"github.com/minio/minio/cmd/logger"
	"github.com/norganna/logeric"
	"google.golang.org/grpc/credentials"
)

// Store is a store for the certificates
type Store struct {
	logger logeric.FieldLogger
	cert   tls.Certificate
	pool   *x509.CertPool
}

// New returns a new certificate store.
func New(logger logeric.FieldLogger) *Store {
	logger = logger.WithField("prefix", "certs")

	return &Store{
		logger: logger,
	}
}

// CreateSelfSignedCert will create new self-signed certs in the given files (but won't load them into store).
func (s *Store) CreateSelfSignedCert(keyFile, certFile, certNames string) {
	private, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		s.logger.WithError(err).Fatal("Cannot generate private key")
	}

	notBefore := time.Now().Add(-60 * time.Second)
	notAfter := notBefore.Add(10 * 365 * 24 * time.Hour) // 10 years
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		s.logger.WithError(err).Fatal("Cannot generate certificate serial")
	}

	template := x509.Certificate{
		Subject: pkix.Name{
			CommonName:   "localhost",
			Organization: []string{"Transit server"},
		},
		SerialNumber: serialNumber,
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		BasicConstraintsValid: true,
	}

	hosts := strings.Split(certNames, ",")
	for _, h := range hosts {
		if h != "" {
			if ip := net.ParseIP(h); ip != nil {
				template.IPAddresses = append(template.IPAddresses, ip)
			} else {
				template.DNSNames = append(template.DNSNames, h)
			}
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, private.Public(), private)
	if err != nil {
		s.logger.WithError(err).Fatal("Failed to create certificate")
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		s.logger.WithError(err).Fatal("Failed to open cert file for writing")
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certOut.Close()

	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		s.logger.WithError(err).Fatal("Failed to open key file for writing")
		return
	}

	keyBytes, err := x509.MarshalECPrivateKey(private)
	if err != nil {
		s.logger.WithError(err).Fatal("Failed to marshall private key")
		return
	}

	pem.Encode(keyOut, &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})
	keyOut.Close()
}

// CheckCreateTLS will check if the certificates exist and create them if not.
func (s *Store) CheckCreateTLS(keyFile, certFile, certNames string) {
	_, keyErr := os.Stat(keyFile)
	_, certErr := os.Stat(certFile)
	if os.IsNotExist(keyErr) && os.IsNotExist(certErr) {
		// Default file names do not exist, create new self-signed certificate for them.
		logger.Info("Creating default certificate files")
		s.CreateSelfSignedCert(keyFile, certFile, certNames)
	} else {
		if keyErr != nil {
			s.logger.WithError(keyErr).Fatal("Error finding key file")
		}
		if certErr != nil {
			s.logger.WithError(certErr).Fatal("Error finding cert file")
		}
	}
}

// LoadTLS will load the TLS certificates from the specified files.
func (s *Store) LoadTLS(keyFile, certFile string) {
	var keyPem, certPem []byte
	var err error

	if keyPem, err = ioutil.ReadFile(keyFile); err != nil {
		s.logger.WithField("keyFile", keyFile).Fatal("Cannot read key file")
	}

	if certPem, err = ioutil.ReadFile(certFile); err != nil {
		s.logger.WithField("certFile", certFile).Fatal("Cannot read cert file")
	}

	s.cert, err = tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		s.logger.WithError(err).Fatal("Failed to combine key pair")
	}

	var certLeaf *x509.Certificate
	certLeaf, err = x509.ParseCertificate(s.cert.Certificate[0])
	if err != nil {
		s.logger.WithError(err).Fatal("Failed to parse first certificate in file")
	}

	s.pool = x509.NewCertPool()
	s.pool.AddCert(certLeaf)
}

// TLSCertificate will return the loaded tls certificate.
func (s *Store) TLSCertificate() tls.Certificate {
	return s.cert
}

// ServerConfig returns the tls config for a server.
func (s *Store) ServerConfig(skipVerify bool) *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{s.cert},
		InsecureSkipVerify: skipVerify,
	}
}

// ServerCredentials returns the credentials needed to start the server.
func (s *Store) ServerCredentials(skipVerify bool) credentials.TransportCredentials {
	return credentials.NewTLS(s.ServerConfig(skipVerify))
}

// ClientCredentials returns the credentials needed to connect a client to the server.
func (s *Store) ClientCredentials() credentials.TransportCredentials {
	return credentials.NewClientTLSFromCert(s.pool, "")
}

// AnonClient returns the credentials needed to connect an anonymous client to the server.
func (s *Store) AnonClient() credentials.TransportCredentials {
	return credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true,
	})
}
