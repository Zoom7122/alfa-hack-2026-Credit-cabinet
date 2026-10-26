package service

import "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"

// TestUsers — синтетические пользователи для входа (GET /auth/test-users).
//
// Первые пять — те же id, что у моков фронта (frontend/src/mocks/data.ts): на них держатся демо-экраны
// рассмотрения, предложений, подписания и рабочего места банка, которые пока на моках.
// Остальные — представители компаний сценариев A–E mock-api (docs/mock-api-guide.md): по ним
// сбор данных возвращает настоящие ответы всех систем.
var TestUsers = []domain.TestUserSeed{
	{
		ID: "usr_director_romashka", FullName: "Петров Пётр Петрович", Role: domain.UserClient,
		Persona:   "Директор ООО «Ромашка»: хорошая история, авто-одобрение, сам подписывает договор",
		Companies: []domain.TestCompanySeed{{ID: "apl_romashka", INN: "7701234567", Name: "ООО «Ромашка»", Role: domain.RoleDirector}},
	},
	{
		ID: "usr_accountant_romashka", FullName: "Иванова Анна Сергеевна", Role: domain.UserClient,
		Persona:   "Бухгалтер ООО «Ромашка»: подаёт заявку, банк запросит доверенность, подписывает директор",
		Companies: []domain.TestCompanySeed{{ID: "apl_romashka", INN: "7701234567", Name: "ООО «Ромашка»", Role: domain.RoleAccountant}},
	},
	{
		ID: "usr_ip_smirnov", FullName: "Смирнов Кирилл Андреевич", Role: domain.UserClient,
		Persona:   "ИП, кофейня 1,5 года: расхождение по выручке, ручное рассмотрение",
		Companies: []domain.TestCompanySeed{{ID: "apl_smirnov", INN: "771234567890", Name: "ИП Смирнов К. А.", Role: domain.RoleDirector}},
	},
	{
		ID: "usr_director_vector", FullName: "Лебедева Вера Олеговна", Role: domain.UserClient,
		Persona:   "Директор ООО «Вектор Строй»: отказ по оборудованию с объяснением, готовое предложение по транспорту",
		Companies: []domain.TestCompanySeed{{ID: "apl_vector", INN: "7724567890", Name: "ООО «Вектор Строй»", Role: domain.RoleDirector}},
	},
	{
		ID: "usr_underwriter_sokolova", FullName: "Соколова Мария Игоревна", Role: domain.UserUnderwriter,
		Persona: "Андеррайтер: очередь ручного рассмотрения, досье и решение по заявке",
	},
	{
		ID: "usr_underwriter_orlov", FullName: "Орлов Дмитрий Андреевич", Role: domain.UserUnderwriter,
		Persona: "Второй андеррайтер: заявку, которую он взял, Соколова решить не может",
	},
	{
		ID: "usr_director_north", FullName: "Ковалёв Игорь Сергеевич", Role: domain.UserClient,
		Persona:   "Сценарий A: директор ООО «Северный ветер», опт продуктов — живой сбор данных, ожидается одобрение",
		Companies: []domain.TestCompanySeed{{ID: "apl_north_wind", INN: "7799000012", Name: "ООО «Северный ветер»", Role: domain.RoleDirector}},
	},
	{
		ID: "usr_accountant_north", FullName: "Литвинова Марина Олеговна", Role: domain.UserClient,
		Persona:   "Сценарий A: бухгалтер ООО «Северный ветер» — подаёт по доверенности (МЧД), подписывает директор",
		Companies: []domain.TestCompanySeed{{ID: "apl_north_wind", INN: "7799000012", Name: "ООО «Северный ветер»", Role: domain.RoleAccountant}},
	},
	{
		ID: "usr_director_stroyresurs", FullName: "Орлов Денис Павлович", Role: domain.UserClient,
		Persona:   "Сценарий B: директор ООО «Стройресурс» — одобрение с меньшей суммой или дольше срок",
		Companies: []domain.TestCompanySeed{{ID: "apl_stroyresurs", INN: "7799000020", Name: "ООО «Стройресурс»", Role: domain.RoleDirector}},
	},
	{
		ID: "usr_director_altair", FullName: "Белов Артём Николаевич", Role: domain.UserClient,
		Persona:   "Сценарий C: директор ООО «Альтаир» — ожидается отказ",
		Companies: []domain.TestCompanySeed{{ID: "apl_altair", INN: "7799000037", Name: "ООО «Альтаир»", Role: domain.RoleDirector}},
	},
	{
		ID: "usr_ip_smirnova", FullName: "Смирнова Анна Викторовна", Role: domain.UserClient,
		Persona:   "Сценарий D: ИП, интернет-торговля — банк спросит про маркетплейсы и доход НПД",
		Companies: []domain.TestCompanySeed{{ID: "apl_ip_smirnova", INN: "779900010202", Name: "ИП Смирнова А. В.", Role: domain.RoleOwner}},
	},
	{
		ID: "usr_finance_techno", FullName: "Кузнецов Павел Андреевич", Role: domain.UserClient,
		Persona:   "Сценарий E: отвечает за финансирование в ООО «Техно-Сервис» — ручное рассмотрение",
		Companies: []domain.TestCompanySeed{{ID: "apl_techno_service", INN: "7799000044", Name: "ООО «Техно-Сервис»", Role: domain.RoleFinanceEmployee}},
	},
}
