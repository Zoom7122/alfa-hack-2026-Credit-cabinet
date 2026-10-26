package postgres

import "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"

type testCompany struct {
	domain.TestCompanySeed
	DirectorID       string
	DirectorName     string
	OGRN             string
	RegistrationDate string
}

type testApplication struct {
	ID          int
	Company     string
	Status      domain.ApplicationStatus
	Product     string
	Amount      int64
	Term        int
	CreatorID   string
	CreatorName string
	AssigneeID  string
	Reason      string
}

// Кабинет и очередь банка — по frontend/src/mocks/data.ts. Компании A–E —
// по service.TestUsers и mock-api. Status описывает прежний демонстрационный снимок;
// новые заявки проходят настоящий сбор и скоринг, их исход определяется движком.
var testCompanies = map[string]testCompany{
	"romashka": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_romashka", INN: "7701234567", Name: "ООО «Ромашка»", Role: domain.RoleDirector},
		DirectorID:      "usr_director_romashka", DirectorName: "Петров Пётр Петрович", OGRN: "1027700000000", RegistrationDate: "2019-03-14",
	},
	"smirnov": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_smirnov", INN: "771234567890", Name: "ИП Смирнов К. А.", Role: domain.RoleDirector},
		DirectorID:      "usr_ip_smirnov", DirectorName: "Смирнов Кирилл Андреевич", OGRN: "323774600012345", RegistrationDate: "2025-04-02",
	},
	"vector": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_vector", INN: "7724567890", Name: "ООО «Вектор Строй»", Role: domain.RoleDirector},
		DirectorID:      "usr_director_vector", DirectorName: "Лебедева Вера Олеговна", OGRN: "1157746000000", RegistrationDate: "2015-06-20",
	},
	"technoline": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_technoline", INN: "7709876501", Name: "ООО «ТехноЛайн»", Role: domain.RoleDirector},
		DirectorID:      "usr_demo_technoline", DirectorName: "Тихонов Артём Сергеевич", RegistrationDate: "2016-04-11",
	},
	"sever_agro": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_sever_agro", INN: "2901122334", Name: "ООО «Север Агро»", Role: domain.RoleDirector},
		DirectorID:      "usr_demo_sever_agro", DirectorName: "Белова Ирина Павловна", RegistrationDate: "2011-08-30",
	},
	"bakery": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_bakery", INN: "5001928374", Name: "ООО «Пекарня №1»", Role: domain.RoleDirector},
		DirectorID:      "usr_demo_bakery", DirectorName: "Громов Олег Ильич", RegistrationDate: "2019-02-05",
	},
	"kim": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_kim", INN: "780198765432", Name: "ИП Ким Е. В.", Role: domain.RoleOwner},
		DirectorID:      "usr_demo_kim", DirectorName: "Ким Елена Викторовна", RegistrationDate: "2026-01-20",
	},
	"beta": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_beta", INN: "7811223344", Name: "ООО «Бета Логистик»", Role: domain.RoleDirector},
		DirectorID:      "usr_demo_beta", DirectorName: "Зуев Пётр Андреевич", RegistrationDate: "2018-10-01",
	},
	"north": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_north_wind", INN: "7799000012", Name: "ООО «Северный ветер»", Role: domain.RoleDirector},
		DirectorID:      "usr_director_north", DirectorName: "Ковалёв Игорь Сергеевич", OGRN: "1217700412345", RegistrationDate: "2021-03-14",
	},
	"stroyresurs": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_stroyresurs", INN: "7799000020", Name: "ООО «Стройресурс»", Role: domain.RoleDirector},
		DirectorID:      "usr_director_stroyresurs", DirectorName: "Орлов Денис Павлович", OGRN: "1197700523456", RegistrationDate: "2019-05-20",
	},
	"altair": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_altair", INN: "7799000037", Name: "ООО «Альтаир»", Role: domain.RoleDirector},
		DirectorID:      "usr_director_altair", DirectorName: "Белов Артём Николаевич", OGRN: "1207700634567", RegistrationDate: "2020-08-17",
	},
	"smirnova": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_ip_smirnova", INN: "779900010202", Name: "ИП Смирнова А. В.", Role: domain.RoleOwner},
		DirectorID:      "usr_ip_smirnova", DirectorName: "Смирнова Анна Викторовна", OGRN: "326774600012345", RegistrationDate: "2026-07-28",
	},
	"techno": {
		TestCompanySeed: domain.TestCompanySeed{ID: "apl_techno_service", INN: "7799000044", Name: "ООО «Техно-Сервис»", Role: domain.RoleFinanceEmployee},
		DirectorID:      "usr_finance_techno", DirectorName: "Зайцева Ольга Михайловна", OGRN: "1187700745678", RegistrationDate: "2018-04-09",
	},
}

var testApplications = []testApplication{
	// SIGNING из браузерного мока заменён на OFFERED: живой сервис ещё не подписывает договоры.
	{ID: 123, Company: "romashka", Status: domain.ApplicationOffered, Product: "WORKING_CAPITAL", Amount: 5_000_000, Term: 24,
		CreatorID: "usr_accountant_romashka", CreatorName: "Иванова Анна Сергеевна"},
	{ID: 131, Company: "romashka", Status: domain.ApplicationWaitingForInfo, Product: "VEHICLE", Amount: 3_200_000, Term: 36,
		CreatorID: "usr_accountant_romashka", CreatorName: "Иванова Анна Сергеевна", Reason: "Подтвердите полномочия представителя"},
	{ID: 140, Company: "romashka", Status: domain.ApplicationDraft, Product: "EQUIPMENT", Amount: 1_800_000, Term: 30},
	{ID: 147, Company: "smirnov", Status: domain.ApplicationManualReview, Product: "WORKING_CAPITAL", Amount: 2_500_000, Term: 18,
		Reason: "Расхождение по выручке в анкете и выписке"},
	{ID: 145, Company: "technoline", Status: domain.ApplicationManualReview, Product: "VEHICLE", Amount: 8_000_000, Term: 36,
		AssigneeID: "usr_underwriter_sokolova", Reason: "Требуется проверка залога по транспорту"},
	{ID: 142, Company: "sever_agro", Status: domain.ApplicationManualReview, Product: "CREDIT_LINE", Amount: 15_000_000, Term: 24,
		Reason: "Сезонность выручки — требуется оценка специалиста"},
	{ID: 139, Company: "bakery", Status: domain.ApplicationManualReview, Product: "EQUIPMENT", Amount: 4_200_000, Term: 30,
		AssigneeID: "usr_underwriter_orlov", Reason: "Нужно проверить стоимость оборудования"},
	{ID: 136, Company: "kim", Status: domain.ApplicationManualReview, Product: "WORKING_CAPITAL", Amount: 900_000, Term: 12,
		Reason: "Короткая история бизнеса"},
	// Вместо зависшего ENRICHING создаём настоящую задачу сбора через outbox → Kafka.
	{ID: 150, Company: "beta", Status: domain.ApplicationSubmitted, Product: "WORKING_CAPITAL", Amount: 6_000_000, Term: 24},
	{ID: 152, Company: "vector", Status: domain.ApplicationRejected, Product: "EQUIPMENT", Amount: 12_000_000, Term: 48,
		Reason: "Долговая нагрузка превышает допустимый уровень"},
	{ID: 155, Company: "vector", Status: domain.ApplicationOffered, Product: "VEHICLE", Amount: 4_000_000, Term: 36},
	{ID: 501, Company: "north", Status: domain.ApplicationOffered, Product: "WORKING_CAPITAL", Amount: 5_000_000, Term: 24},
	{ID: 502, Company: "north", Status: domain.ApplicationWaitingForInfo, Product: "WORKING_CAPITAL", Amount: 5_000_000, Term: 24,
		CreatorID: "usr_accountant_north", CreatorName: "Литвинова Марина Олеговна", Reason: "Подтвердите полномочия представителя"},
	{ID: 503, Company: "stroyresurs", Status: domain.ApplicationManualReview, Product: "WORKING_CAPITAL", Amount: 8_000_000, Term: 36,
		Reason: "Высокая долговая нагрузка и много кредитных обращений"},
	{ID: 504, Company: "altair", Status: domain.ApplicationRejected, Product: "WORKING_CAPITAL", Amount: 5_000_000, Term: 24,
		Reason: "Подтверждены банкротство заёмщика и текущая просрочка"},
	{ID: 505, Company: "smirnova", Status: domain.ApplicationWaitingForInfo, Product: "WORKING_CAPITAL", Amount: 1_000_000, Term: 12,
		Reason: "Уточните свободные деньги и предоставьте выписку"},
	{ID: 506, Company: "techno", Status: domain.ApplicationManualReview, Product: "WORKING_CAPITAL", Amount: 5_000_000, Term: 24,
		CreatorID: "usr_finance_techno", CreatorName: "Кузнецов Павел Андреевич", Reason: "Не подтверждены полномочия представителя"},
}
