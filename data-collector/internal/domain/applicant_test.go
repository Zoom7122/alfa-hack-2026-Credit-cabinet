package domain_test

import (
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

func TestRepresentativeRoles(t *testing.T) {
	for _, r := range []string{"OWNER", "DIRECTOR", "CFO", "ACCOUNTANT", "FINANCE_EMPLOYEE", "BANKING_EMPLOYEE", "AUTHORIZED_EMPLOYEE"} {
		if !domain.RepresentativeRole(r).Valid() {
			t.Errorf("%s must be valid", r)
		}
	}
	for _, r := range []string{"", "CEO", "director"} {
		if domain.RepresentativeRole(r).Valid() {
			t.Errorf("%q must be invalid", r)
		}
	}
}

func TestCanSign(t *testing.T) {
	ul, ip := domain.CompanyTypeByINN("7799000012"), domain.CompanyTypeByINN("779900010202")
	if ul != domain.CompanyLegalEntity || ip != domain.CompanySoleProprietor {
		t.Fatalf("types: %s %s", ul, ip)
	}
	cases := []struct {
		role domain.RepresentativeRole
		t    domain.CompanyType
		want bool
	}{
		{domain.RoleDirector, ul, true},
		{domain.RoleOwner, ul, false}, // собственнику ООО нужна доверенность
		{domain.RoleOwner, ip, true},  // ИП подписывает сам
		{domain.RoleDirector, ip, true},
		{domain.RoleCFO, ul, false},
		{domain.RoleAccountant, ip, false},
		{domain.RoleFinanceEmployee, ul, false},
		{domain.RoleBankingEmployee, ul, false},
		{domain.RoleAuthorizedEmployee, ul, false},
	}
	for _, c := range cases {
		if got := domain.CanSign(c.role, c.t); got != c.want {
			t.Errorf("CanSign(%s, %s) = %v, want %v", c.role, c.t, got, c.want)
		}
	}
}

func TestCompanyCardName(t *testing.T) {
	if n := (domain.CompanyCard{ShortName: `ООО "А"`, FullName: `ОБЩЕСТВО "А"`}).Name(); n != `ООО "А"` {
		t.Errorf("name = %q", n)
	}
	if n := (domain.CompanyCard{FullName: "ИП Смирнова"}).Name(); n != "ИП Смирнова" {
		t.Errorf("name = %q", n)
	}
}
