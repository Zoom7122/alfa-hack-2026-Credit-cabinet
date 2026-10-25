package signature

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// TrustStore — доверенные корни, промежуточные УЦ и списки отзыва профиля проверки.
// В профиле TEST это локальный тестовый УЦ и его CRL; системные корни ОС не используются никогда.
type TrustStore struct {
	roots         []*x509.Certificate
	intermediates []*x509.Certificate
	crls          []*x509.RevocationList
}

// NewTrustStore — хранилище из готовых сертификатов и CRL (тесты, демо).
func NewTrustStore(roots, intermediates []*x509.Certificate, crls []*x509.RevocationList) *TrustStore {
	return &TrustStore{roots: roots, intermediates: intermediates, crls: crls}
}

// LoadTrustStore читает папку: *.pem, *.crt, *.cer — сертификаты (самоподписанный УЦ — корень,
// остальные — промежуточные), *.crl — списки отзыва; PEM или DER. Пустая dir — без корней:
// тогда цепочку не построить и сертификат всегда INDETERMINATE.
func LoadTrustStore(dir string) (*TrustStore, error) {
	ts := &TrustStore{}
	if dir == "" {
		return ts, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("trust store: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".pem", ".crt", ".cer":
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("trust store: %w", err)
			}
			certs, err := parseCertificates(data)
			if err != nil {
				return nil, fmt.Errorf("trust store %s: %w", e.Name(), err)
			}
			for _, c := range certs {
				if !c.IsCA {
					return nil, fmt.Errorf("trust store %s: %s — не сертификат УЦ", e.Name(), c.Subject)
				}
				if bytes.Equal(c.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(c) == nil {
					ts.roots = append(ts.roots, c)
				} else {
					ts.intermediates = append(ts.intermediates, c)
				}
			}
		case ".crl":
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("trust store: %w", err)
			}
			if block, _ := pem.Decode(data); block != nil {
				data = block.Bytes
			}
			crl, err := x509.ParseRevocationList(data)
			if err != nil {
				return nil, fmt.Errorf("trust store %s: %w", e.Name(), err)
			}
			ts.crls = append(ts.crls, crl)
		}
	}
	return ts, nil
}

// Summary — что загружено, для лога при старте.
func (ts *TrustStore) Summary() (roots, intermediates, crls int) {
	return len(ts.roots), len(ts.intermediates), len(ts.crls)
}

func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	if !bytes.Contains(data, []byte("-----BEGIN")) {
		c, err := x509.ParseCertificate(data)
		if err != nil {
			return nil, err
		}
		return []*x509.Certificate{c}, nil
	}
	var out []*x509.Certificate
	for rest := data; ; {
		var block *pem.Block
		if block, rest = pem.Decode(rest); block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("нет сертификатов")
	}
	return out, nil
}

// chainResult — итог построения цепочки до доверенного корня.
type chainResult struct {
	chain  []*x509.Certificate // подписант → … → корень
	status domain.VerificationStatus
	msg    string
}

// buildChain строит цепочку от сертификата подписанта до корня профиля. Время проверки
// цепочки — внутри срока действия сертификата подписанта: срок проверяется отдельно, чтобы
// «просрочен» и «не доверен» не смешивались.
func (ts *TrustStore) buildChain(leaf *x509.Certificate, extra []*x509.Certificate, at time.Time) chainResult {
	if len(ts.roots) == 0 {
		return chainResult{status: statusIndeterminate, msg: "доверенные корни профиля не настроены (SIGNATURE_TRUST_DIR)"}
	}
	roots, intermediates := x509.NewCertPool(), x509.NewCertPool()
	for _, c := range ts.roots {
		roots.AddCert(c)
	}
	for _, c := range append(slices.Clone(ts.intermediates), extra...) {
		if c != leaf {
			intermediates.AddCert(c)
		}
	}
	chainTime := at
	if chainTime.After(leaf.NotAfter) {
		chainTime = leaf.NotAfter
	}
	if chainTime.Before(leaf.NotBefore) {
		chainTime = leaf.NotBefore
	}
	chains, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots, // не nil: системные корни не подмешиваются
		Intermediates: intermediates,
		CurrentTime:   chainTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err == nil {
		return chainResult{chain: chains[0], status: statusVerified,
			msg: "цепочка до доверенного корня: " + chainSubjects(chains[0])}
	}
	var (
		unknown  x509.UnknownAuthorityError
		invalid  x509.CertificateInvalidError
		critical x509.UnhandledCriticalExtension
		insecure x509.InsecureAlgorithmError
	)
	switch {
	case errors.As(err, &unknown):
		return chainResult{status: statusInvalid, msg: "сертификат выдан не доверенным УЦ профиля: цепочка не строится до корня"}
	case errors.As(err, &invalid):
		return chainResult{status: statusInvalid, msg: "цепочка сертификатов недействительна: " + err.Error()}
	case errors.As(err, &critical):
		return chainResult{status: statusIndeterminate, msg: "в сертификате критическое расширение, которое профиль не поддерживает"}
	case errors.As(err, &insecure), errors.Is(err, x509.ErrUnsupportedAlgorithm):
		return chainResult{status: statusIndeterminate, msg: "алгоритм подписи сертификатов в цепочке не поддерживается (ГОСТ или устаревший)"}
	}
	return chainResult{status: statusIndeterminate, msg: "цепочку не удалось проверить: " + err.Error()}
}

func chainSubjects(chain []*x509.Certificate) string {
	names := make([]string, 0, len(chain))
	for _, c := range chain {
		names = append(names, displayName(c))
	}
	return strings.Join(names, " → ")
}

// displayName — короткое имя сертификата: CN, иначе всё имя субъекта.
func displayName(c *x509.Certificate) string {
	if c.Subject.CommonName != "" {
		return c.Subject.CommonName
	}
	return c.Subject.String()
}

// revocation — статус отзыва одного сертификата.
type revocation struct {
	status     string // GOOD, REVOKED, UNKNOWN
	revokedAt  time.Time
	thisUpdate time.Time
	nextUpdate time.Time
	msg        string
}

// revocationStatus проверяет сертификат по CRL его издателя на момент at. Подходит CRL,
// подписанный издателем и действующий на at (thisUpdate ≤ at ≤ nextUpdate); из нескольких —
// самый свежий. Нет такого CRL — UNKNOWN.
func (ts *TrustStore) revocationStatus(cert, issuer *x509.Certificate, at time.Time) revocation {
	var best *x509.RevocationList
	stale := false
	for _, crl := range ts.crls {
		if !bytes.Equal(crl.RawIssuer, issuer.RawSubject) || crl.CheckSignatureFrom(issuer) != nil {
			continue
		}
		if crl.ThisUpdate.After(at) || crl.NextUpdate.IsZero() || at.After(crl.NextUpdate) {
			stale = true
			continue
		}
		if best == nil || crl.ThisUpdate.After(best.ThisUpdate) {
			best = crl
		}
	}
	if best == nil {
		msg := "нет списка отзыва (CRL) от " + displayName(issuer)
		if stale {
			msg = "список отзыва " + displayName(issuer) + " устарел или ещё не действует"
		}
		return revocation{status: "UNKNOWN", msg: msg}
	}
	r := revocation{status: "GOOD", thisUpdate: best.ThisUpdate.UTC(), nextUpdate: best.NextUpdate.UTC()}
	for _, e := range best.RevokedCertificateEntries {
		if e.SerialNumber != nil && e.SerialNumber.Cmp(cert.SerialNumber) == 0 && !e.RevocationTime.After(at) {
			r.status, r.revokedAt = "REVOKED", e.RevocationTime.UTC()
			r.msg = fmt.Sprintf("сертификат %s отозван %s (CRL %s)", displayName(cert), r.revokedAt.Format(time.DateOnly), displayName(issuer))
			return r
		}
	}
	r.msg = fmt.Sprintf("%s не отозван (CRL %s от %s)", displayName(cert), displayName(issuer), r.thisUpdate.Format(time.DateOnly))
	return r
}
