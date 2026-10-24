package compliance_test

import (
	"net/http"
	"testing"

	compliance "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Compliance"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/compliance/v1", compliance.Register)

var allLists = []string{"RFM_TERRORISM_EXTREMISM", "RFM_FREEZE", "OFAC_SDN", "UK", "EU"}

func TestZSK(t *testing.T) {
	want := map[string]string{mt.InnA: "LOW", mt.InnB: "LOW", mt.InnC: "LOW", mt.InnD: "LOW", mt.InnE: "MEDIUM"}
	for inn, lvl := range want {
		r := mt.Decode[struct {
			INN         string   `json:"inn"`
			RiskLevel   string   `json:"risk_level"`
			ReasonCodes []string `json:"reason_codes"`
		}](t, mt.Get(router, "/compliance/v1/zsk/"+inn), http.StatusOK)
		if r.INN != inn || r.RiskLevel != lvl || (lvl == "MEDIUM") != (len(r.ReasonCodes) > 0) {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	mt.ErrorCode(t, mt.Get(router, "/compliance/v1/zsk/"+mt.Unknown), http.StatusNotFound)
	mt.ErrorCode(t, mt.Get(router, "/compliance/v1/zsk/12"), http.StatusBadRequest)
}

type match struct {
	List       string  `json:"list"`
	EntryID    string  `json:"entry_id"`
	MatchScore float64 `json:"match_score"`
}

type screening struct {
	ListVersions map[string]string `json:"list_versions"`
	Results      []struct {
		Ref     string  `json:"ref"`
		Listed  bool    `json:"listed"`
		Matches []match `json:"matches"`
	} `json:"results"`
}

func screen(t *testing.T, lists []string, subjects ...map[string]any) screening {
	t.Helper()
	return mt.Decode[screening](t, mt.PostJSON(t, router, "/compliance/v1/screening",
		map[string]any{"lists": lists, "subjects": subjects}), http.StatusOK)
}

func legal(ref, inn, name string) map[string]any {
	return map[string]any{"ref": ref, "type": "LEGAL", "inn": inn, "name": name}
}

func person(ref, inn, name, birth string) map[string]any {
	return map[string]any{"ref": ref, "type": "PERSON", "inn": inn, "full_name": name, "birth_date": birth}
}

// Ни одна компания и ни один директор сценариев A–E не в списках, хотя тёзки там есть.
func TestScenariosClean(t *testing.T) {
	r := screen(t, allLists,
		legal("A", mt.InnA, `ООО "СЕВЕРНЫЙ ВЕТЕР"`),
		person("A-dir", mt.DirA, "Ковалёв Игорь Сергеевич", "1984-07-19"),
		person("B-dir", mt.DirB, "Орлов Денис Павлович", "1979-02-11"),
		legal("C", mt.InnC, `ООО "АЛЬТАИР"`),
		person("C-dir", mt.DirC, "Белов Артём Николаевич", "1988-11-03"),
		legal("E", mt.InnE, `ООО "ТЕХНО-СЕРВИС"`),
	)
	if len(r.Results) != 6 || len(r.ListVersions) != len(allLists) {
		t.Fatalf("%+v", r)
	}
	for _, res := range r.Results {
		if res.Listed || res.Matches == nil || len(res.Matches) != 0 {
			t.Errorf("%s: %+v", res.Ref, res)
		}
	}
}

func TestMatches(t *testing.T) {
	r := screen(t, allLists,
		person("by-name", "", "Громов Виктор Андреевич", "1979-03-02"),
		legal("by-inn", "7799000099", "Какое-то другое название"),
		person("other-birth", "", "Громов Виктор Андреевич", "1980-01-01"),
	)
	byName, byINN, other := r.Results[0], r.Results[1], r.Results[2]
	if !byName.Listed || len(byName.Matches) != 1 || byName.Matches[0].EntryID != "RFM-55821" ||
		byName.Matches[0].MatchScore != 0.97 {
		t.Errorf("by name: %+v", byName)
	}
	if !byINN.Listed || len(byINN.Matches) != 2 || byINN.Matches[0].MatchScore != 1 {
		t.Errorf("by inn: %+v", byINN)
	}
	if other.Listed {
		t.Errorf("namesake with another birth date matched: %+v", other)
	}

	// проверяются только запрошенные списки
	only := screen(t, []string{"OFAC_SDN"}, person("p", "", "Громов Виктор Андреевич", "1979-03-02"))
	if only.Results[0].Listed || len(only.ListVersions) != 1 {
		t.Errorf("only OFAC: %+v", only)
	}
}

func TestScreeningErrors(t *testing.T) {
	bodies := []any{
		"{",
		map[string]any{"lists": []string{}, "subjects": []any{person("p", "", "X Y", "")}},
		map[string]any{"lists": allLists, "subjects": []any{}},
		map[string]any{"lists": []string{"NOPE"}, "subjects": []any{person("p", "", "X Y", "")}},
		map[string]any{"lists": allLists, "subjects": []any{map[string]any{"ref": "x", "type": "ROBOT"}}},
	}
	for i, b := range bodies {
		if code := mt.ErrorCode(t, mt.PostJSON(t, router, "/compliance/v1/screening", b), http.StatusBadRequest); code != mockkit.CodeBadRequest {
			t.Errorf("#%d: code %s", i, code)
		}
	}
}
