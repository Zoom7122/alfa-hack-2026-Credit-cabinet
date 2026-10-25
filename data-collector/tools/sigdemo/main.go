// sigdemo выпускает тестовый УЦ профиля TEST и подписывает демо-файлы для worker'а подписи:
//
//	go run ./tools/sigdemo -trust testdata/signature/trust -examples examples/files
//
// В -trust пишутся корень (root.pem) и список отзыва (root.crl) — их монтирует worker подписи
// в docker-compose. В -examples — открепленная подпись balance.pdf.sig для сценария A и пары
// «документ + подпись» в signature-demo/ с разными исходами проверки. Закрытые ключи живут только
// в памяти этого запуска: чтобы подписать что-то ещё, запустите sigdemo заново — корень сменится.
// Это тестовые сертификаты, а не КЭП: их проверка — профиль TEST, не проверка УКЭП.
package main

import (
	"bytes"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/signature/sigtest"
)

func main() {
	trustDir := flag.String("trust", "testdata/signature/trust", "куда положить root.pem и root.crl")
	examplesDir := flag.String("examples", "examples/files", "папка с balance.pdf")
	flag.Parse()
	if err := run(*trustDir, *examplesDir); err != nil {
		log.Fatal(err)
	}
}

func run(trustDir, examplesDir string) error {
	now := time.Now().UTC().Truncate(time.Second)
	from, until := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	ca, err := sigtest.NewCA("Тестовый корневой УЦ (TEST)", from, time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return err
	}
	issue := func(s sigtest.Subject) *sigtest.Signer {
		if err != nil {
			return nil
		}
		var signer *sigtest.Signer
		signer, err = ca.Issue(s, sigtest.CertOptions{NotBefore: from, NotAfter: until})
		return signer
	}
	// люди из сидов mock-api: представитель с МЧД 0f5a2c1e… и директор «Северного ветра», директор «Стройресурса»
	accountant := sigtest.Subject{Surname: "Литвинова", GivenName: "Марина Олеговна", Title: "Главный бухгалтер",
		INN: "779900010333", SNILS: "148-562-907 41"}
	litvinova, revokedLitvinova := issue(accountant), issue(accountant)
	kovalev := issue(sigtest.Subject{Surname: "Ковалёв", GivenName: "Игорь Сергеевич", Title: "Генеральный директор",
		Organization: `ООО "СЕВЕРНЫЙ ВЕТЕР"`, INN: "779900010139", OrgINN: "7799000012", OGRN: "1217700412345"})
	orlov := issue(sigtest.Subject{Surname: "Орлов", GivenName: "Денис Павлович", Title: "Генеральный директор",
		Organization: `ООО "СТРОЙРЕСУРС"`, INN: "779900020136", OrgINN: "7799000020", OGRN: "1197700523456"})
	if err != nil {
		return err
	}
	crl, err := ca.CRL(now.Add(-time.Hour), time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
		sigtest.Revoked{Cert: revokedLitvinova.Cert, At: now.Add(-time.Hour)})
	if err != nil {
		return err
	}

	balance, err := os.ReadFile(filepath.Join(examplesDir, "balance.pdf"))
	if err != nil {
		return err
	}
	tampered := append(bytes.Clone(balance), []byte("% изменено после подписания\n")...)

	files := map[string][]byte{
		filepath.Join(trustDir, "root.pem"): pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Cert.Raw}),
		filepath.Join(trustDir, "root.crl"): pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crl}),
	}
	demo := filepath.Join(examplesDir, "signature-demo")
	for _, f := range []struct {
		path   string
		doc    []byte // что положить рядом с подписью; nil — документ уже есть
		signed []byte // что подписать
		signer *sigtest.Signer
	}{
		{filepath.Join(examplesDir, "balance.pdf"), nil, balance, litvinova},
		{filepath.Join(demo, "balance-director.pdf"), balance, balance, kovalev},
		{filepath.Join(demo, "balance-tampered.pdf"), tampered, balance, kovalev},
		{filepath.Join(demo, "balance-revoked.pdf"), balance, balance, revokedLitvinova},
		{filepath.Join(demo, "balance-other-company.pdf"), balance, balance, orlov},
	} {
		sig, err := sigtest.Sign(f.signed, f.signer, sigtest.SignOptions{SigningTime: now})
		if err != nil {
			return err
		}
		if f.doc != nil {
			files[f.path] = f.doc
		}
		files[f.path+".sig"] = sig
	}
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		fmt.Println(path)
	}
	return nil
}
