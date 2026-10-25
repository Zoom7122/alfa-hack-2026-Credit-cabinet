package hh_test

import (
	"net/http"
	"net/url"
	"testing"

	hh "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/HH"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/hh", hh.Register)

type found struct {
	Found int `json:"found"`
	Items []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		OpenVacancies int    `json:"open_vacancies"`
		Employer      struct {
			ID string `json:"id"`
		} `json:"employer"`
	} `json:"items"`
}

func TestEmployers(t *testing.T) {
	r := mt.Decode[found](t, mt.Get(router, "/hh/employers?text="+url.QueryEscape("северный ветер")), http.StatusOK)
	if r.Found != 1 || r.Items[0].ID != "9871234" || r.Items[0].OpenVacancies != 3 {
		t.Fatalf("A: %+v", r)
	}
	// по «Альтаир» находятся и сама компания, и тёзка
	if r := mt.Decode[found](t, mt.Get(router, "/hh/employers?text="+url.QueryEscape("Альтаир")), http.StatusOK); r.Found != 2 {
		t.Errorf("Altair: %+v", r)
	}
	if r := mt.Decode[found](t, mt.Get(router, "/hh/employers?text="+url.QueryEscape("Смирнова")), http.StatusOK); r.Found != 0 || r.Items == nil {
		t.Errorf("D: %+v", r)
	}
	e := mt.Decode[struct {
		Name string `json:"name"`
	}](t, mt.Get(router, "/hh/employers/9915502"), http.StatusOK)
	if e.Name != "СТРОЙРЕСУРС" {
		t.Errorf("B: %+v", e)
	}
	mt.ErrorCode(t, mt.Get(router, "/hh/employers"), http.StatusBadRequest)
	mt.ErrorCode(t, mt.Get(router, "/hh/employers/1"), http.StatusNotFound)
}

func TestVacancies(t *testing.T) {
	v := mt.Decode[found](t, mt.Get(router, "/hh/vacancies?employer_id=9871234"), http.StatusOK)
	if v.Found != 3 || v.Items[0].Employer.ID != "9871234" {
		t.Fatalf("A: %+v", v)
	}
	if c := mt.Decode[found](t, mt.Get(router, "/hh/vacancies?employer_id=9640117"), http.StatusOK); c.Found != 0 || c.Items == nil {
		t.Errorf("C: %+v", c)
	}
	mt.ErrorCode(t, mt.Get(router, "/hh/vacancies"), http.StatusBadRequest)
}
