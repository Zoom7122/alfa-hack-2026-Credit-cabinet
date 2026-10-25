package mockapi

import (
	"encoding/json"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Разбор собранных ответов ФНС и реестра МЧД (колонки fns и m4d в raw_collections) для
// проверки полномочий подписанта. Форматы — как у mock-api и реальных сервисов.

// RegistryHeads — руководители компании по данным ФНС: ИНН и ФИО — из «Прозрачного бизнеса»
// (pb.vyp.masruk), без него — только ФИО из ЕГРЮЛ (поле g). У ИП руководитель — сам
// предприниматель (строка ЕГРИП). false — ни ЕГРЮЛ, ни «Прозрачный бизнес» не ответили.
func RegistryHeads(fnsData json.RawMessage) ([]domain.RegistryHead, bool) {
	var d struct {
		EGRUL *struct {
			Rows []struct {
				N string `json:"n"` // полное имя; у ИП — «Индивидуальный предприниматель ФИО»
				I string `json:"i"`
				G string `json:"g"` // «ГЕНЕРАЛЬНЫЙ ДИРЕКТОР: ФИО»
				K string `json:"k"` // ul — юрлицо, fl — ИП
			} `json:"rows"`
		} `json:"egrul"`
		PB *struct {
			Vyp struct {
				Masruk []struct {
					INN      string `json:"inn"`
					Name     string `json:"name"`
					Position string `json:"position"`
				} `json:"masruk"`
			} `json:"vyp"`
		} `json:"pb"`
	}
	if len(fnsData) == 0 || json.Unmarshal(fnsData, &d) != nil || (d.EGRUL == nil && d.PB == nil) {
		return nil, false
	}
	var heads []domain.RegistryHead
	if d.PB != nil {
		for _, r := range d.PB.Vyp.Masruk {
			heads = append(heads, domain.RegistryHead{INN: r.INN, FullName: r.Name, Position: r.Position})
		}
	}
	if d.EGRUL != nil && len(d.EGRUL.Rows) > 0 {
		row := d.EGRUL.Rows[0]
		switch {
		case row.K == "fl":
			heads = append(heads, domain.RegistryHead{INN: row.I, Position: "ИНДИВИДУАЛЬНЫЙ ПРЕДПРИНИМАТЕЛЬ",
				FullName: strings.TrimSpace(strings.TrimPrefix(row.N, "Индивидуальный предприниматель"))})
		case len(heads) == 0 && row.G != "":
			if position, name, ok := strings.Cut(row.G, ":"); ok {
				heads = append(heads, domain.RegistryHead{FullName: strings.TrimSpace(name), Position: strings.TrimSpace(position)})
			}
		}
	}
	return heads, true
}

// PowersOfAttorney — МЧД из ответов реестра: поиск по компании (search.items) и доверенность
// по номеру из анкеты (poa), без повторов. false — реестр не ответил ни на один запрос.
func PowersOfAttorney(m4dData json.RawMessage) ([]domain.PowerOfAttorney, bool) {
	var d struct {
		Search *struct {
			Items []json.RawMessage `json:"items"`
		} `json:"search"`
		POA json.RawMessage `json:"poa"`
	}
	if len(m4dData) == 0 || json.Unmarshal(m4dData, &d) != nil {
		return nil, false
	}
	var raws []json.RawMessage
	if d.Search != nil {
		raws = append(raws, d.Search.Items...)
	}
	if len(d.POA) > 0 && string(d.POA) != "null" {
		raws = append(raws, d.POA)
	}
	if d.Search == nil && len(raws) == 0 {
		return nil, false
	}
	var out []domain.PowerOfAttorney
	seen := map[string]bool{}
	for _, raw := range raws {
		var p struct {
			Number     string `json:"number"`
			Status     string `json:"status"`
			ValidFrom  string `json:"valid_from"`
			ValidUntil string `json:"valid_until"`
			RevokedAt  string `json:"revoked_at"`
			Principal  struct {
				INN string `json:"inn"`
			} `json:"principal"`
			Representative struct {
				INN      string `json:"inn"`
				SNILS    string `json:"snils"`
				FullName string `json:"full_name"`
			} `json:"representative"`
			Powers []struct {
				Code string `json:"code"`
			} `json:"powers"`
		}
		if json.Unmarshal(raw, &p) != nil || p.Number == "" || seen[p.Number] {
			continue
		}
		seen[p.Number] = true
		poa := domain.PowerOfAttorney{Number: p.Number, Status: p.Status, ValidFrom: p.ValidFrom, ValidUntil: p.ValidUntil,
			RevokedAt: p.RevokedAt, PrincipalINN: p.Principal.INN, RepresentativeINN: p.Representative.INN,
			RepresentativeSNILS: p.Representative.SNILS, RepresentativeName: p.Representative.FullName}
		for _, pw := range p.Powers {
			poa.Powers = append(poa.Powers, pw.Code)
		}
		out = append(out, poa)
	}
	return out, true
}
