package main

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Transport security.
//
// The agent always speaks TLS 1.3.  On first start it generates a self-signed
// ECDSA P-256 certificate (config agentCert / agentKey).  Clients do not use
// the system CA store; instead they pin the agent certificate's SHA-256
// fingerprint in a known_agents file, exactly like SSH's known_hosts:
//
//   - first connection to an address: the fingerprint is recorded
//     (trust-on-first-use) and a NOTICE is printed, unless trustNewAgents is
//     false, in which case the connection is refused until the fingerprint is
//     added by hand;
//   - later connections: refused if the fingerprint differs.
//
// "ship-grip-fim fingerprint" prints the local agent's fingerprint so it can
// be compared or pre-pinned out of band.

const certValidity = 10 * 365 * 24 * time.Hour

// loadOrCreateAgentCert loads the agent certificate, generating a self-signed
// one when the files do not exist yet.  created reports whether it was new.
func loadOrCreateAgentCert(certPath, keyPath string) (cert tls.Certificate, created bool, err error) {
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	if certErr == nil && keyErr == nil {
		cert, err = tls.LoadX509KeyPair(certPath, keyPath)
		return cert, false, err
	}
	if certErr == nil || keyErr == nil {
		return cert, false, fmt.Errorf("found only one of %s / %s; remove it to regenerate the pair", certPath, keyPath)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return cert, false, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return cert, false, err
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "ship-grip-fim-agent"
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host, Organization: []string{"ship-grip-fim agent"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(certValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return cert, false, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return cert, false, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return cert, false, err
	}
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return cert, false, err
	}
	cert, err = tls.X509KeyPair(certPEM, keyPEM)
	return cert, true, err
}

// certFingerprint returns "SHA256:<hex>" for a DER-encoded certificate.
func certFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "SHA256:" + hex.EncodeToString(sum[:])
}

// agentTLSConfig is the server-side TLS configuration.
func agentTLSConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
}

// cmdFingerprint prints the local agent certificate fingerprint (creating
// the certificate if needed, exactly as the agent would on start).
func cmdFingerprint(config configInfo) error {
	cert, created, err := loadOrCreateAgentCert(config.agentCert, config.agentKey)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("Generated new agent certificate: %s / %s\n", config.agentCert, config.agentKey)
	}
	fmt.Println(certFingerprint(cert.Certificate[0]))
	return nil
}

// ── known agents store ────────────────────────────────────────────────────────

var knownAgentsMu sync.Mutex

// lookupKnownAgent returns the pinned fingerprint for addr, if any.
func lookupKnownAgent(path, addr string) (fp string, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 2 && !strings.HasPrefix(fields[0], "#") && fields[0] == addr {
			return fields[1], true, nil
		}
	}
	return "", false, s.Err()
}

// rememberAgent appends a pinned fingerprint for addr.
func rememberAgent(path, addr, fp string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %s\n", addr, fp)
	return err
}

// dialAgent opens a TLS connection to an agent and verifies its certificate
// against the known_agents file (see the comment at the top of this file).
func dialAgent(config configInfo, host, port string) (net.Conn, error) {
	addr := net.JoinHostPort(host, port)
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS13,
		// Certificate identity is checked by fingerprint pinning in
		// VerifyPeerCertificate below, not by the system CA store.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("agent presented no certificate")
			}
			fp := certFingerprint(raw[0])

			knownAgentsMu.Lock()
			defer knownAgentsMu.Unlock()
			known, found, err := lookupKnownAgent(config.knownAgents, addr)
			if err != nil {
				return fmt.Errorf("reading %s: %w", config.knownAgents, err)
			}
			if found {
				if known != fp {
					return fmt.Errorf("certificate fingerprint for %s has CHANGED\n"+
						"  pinned:  %s\n  offered: %s\n"+
						"  This could be a re-generated agent certificate or a man-in-the-middle.\n"+
						"  If the agent was legitimately reinstalled, delete its line from %s and reconnect.",
						addr, known, fp, config.knownAgents)
				}
				return nil
			}
			if !config.trustNewAgents {
				return fmt.Errorf("agent %s is not in %s (offered %s) and trustNewAgents is false; "+
					"verify the fingerprint with 'ship-grip-fim fingerprint' on the agent host and add the line \"%s %s\"",
					addr, config.knownAgents, fp, addr, fp)
			}
			if err := rememberAgent(config.knownAgents, addr, fp); err != nil {
				return fmt.Errorf("saving fingerprint to %s: %w", config.knownAgents, err)
			}
			fmt.Fprintf(os.Stderr, "NOTICE - first connection to agent %s: pinned fingerprint %s in %s\n", addr, fp, config.knownAgents)
			return nil
		},
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
}
