package mockkit

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"strings"
)

type cabinetCompany struct {
	inn, source, name, directorINN, director, registered, ogrn string
}

// The older cabinet examples use their own identities. Reuse the financial
// scenarios, rebinding the company and its director consistently in every mock.
// Unknown INNs still follow each provider's normal not-found behaviour.
var cabinetCompanies = []cabinetCompany{
	{"7701234567", "7799000012", "РОМАШКА", "770112345695", "Петров Пётр Петрович", "2019-03-14", "1027700000000"},
	{"7724567890", "7799000020", "ВЕКТОР СТРОЙ", "772412345678", "Лебедева Вера Олеговна", "2015-06-20", "1157746000000"},
	{"7709876501", "7799000020", "ТЕХНОЛАЙН", "770912345678", "Тихонов Артём Сергеевич", "2016-04-11", "1167700000001"},
	{"2901122334", "7799000012", "СЕВЕР АГРО", "290112345678", "Белова Ирина Павловна", "2011-08-30", "1112900000001"},
	{"5001928374", "7799000012", "ПЕКАРНЯ №1", "500112345678", "Громов Олег Ильич", "2019-02-05", "1195000000001"},
	{"7811223344", "7799000012", "БЕТА ЛОГИСТИК", "781112345678", "Зуев Пётр Андреевич", "2018-10-01", "1187800000001"},
	{"771234567890", "779900010202", "", "771234567890", "Смирнов Кирилл Андреевич", "2025-04-02", "323774600012345"},
	{"780198765432", "779900010202", "", "780198765432", "Ким Елена Викторовна", "2026-01-20", "326780000000001"},
}

var cabinetSources = map[string]cabinetCompany{
	"7799000012":   {inn: "7799000012", name: "СЕВЕРНЫЙ ВЕТЕР", directorINN: "779900010139", director: "Ковалёв Игорь Сергеевич", registered: "2021-03-14", ogrn: "1217700412345"},
	"7799000020":   {inn: "7799000020", name: "СТРОЙРЕСУРС", directorINN: "779900020136", director: "Орлов Денис Павлович", registered: "2019-05-20", ogrn: "1197700523456"},
	"779900010202": {inn: "779900010202", directorINN: "779900010202", director: "Смирнова Анна Викторовна", registered: "2026-07-28", ogrn: "326774600012345"},
}

// CabinetSeeds extends only providers keyed by borrower/director INN. Property
// registries keep Seeds: copying their VINs/cadastral numbers would duplicate assets.
func CabinetSeeds(fsys fs.FS, dir string) map[string][]byte {
	seeds := Seeds(fsys, dir)
	addCabinetSeeds(seeds)
	return seeds
}

func addCabinetSeeds(seeds map[string][]byte) {
	for _, target := range cabinetCompanies {
		source := cabinetSources[target.source]
		pairs := []string{source.inn, target.inn, source.directorINN, target.directorINN,
			source.director, target.director, strings.ToUpper(source.director), strings.ToUpper(target.director),
			source.registered, target.registered, source.ogrn, target.ogrn}
		pairs = append(pairs, registryDate(source.registered), registryDate(target.registered))
		sourceNames, targetNames := strings.Fields(source.director), strings.Fields(target.director)
		for i := range sourceNames {
			pairs = append(pairs, sourceNames[i], targetNames[i])
		}
		if source.name != "" {
			pairs = append(pairs, source.name, target.name)
		}
		replacer := strings.NewReplacer(pairs...)
		for key, from := range map[string]string{target.inn: source.inn, target.directorINN: source.directorINN} {
			if _, exists := seeds[key]; exists {
				continue // Explicit fixtures always win.
			}
			body, exists := seeds[from]
			if !exists {
				continue // This provider has no fixture of this kind.
			}
			var value any
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				panic(err)
			}
			rebound, err := json.Marshal(rebindSeed(value, replacer))
			if err != nil {
				panic(err)
			}
			seeds[key] = rebound
		}
	}
}

func registryDate(iso string) string {
	return iso[8:10] + "." + iso[5:7] + "." + iso[:4]
}

func rebindSeed(value any, replacer *strings.Replacer) any {
	switch v := value.(type) {
	case string:
		return replacer.Replace(v)
	case []any:
		for i := range v {
			v[i] = rebindSeed(v[i], replacer)
		}
	case map[string]any:
		rebound := make(map[string]any, len(v))
		for key, item := range v {
			rebound[replacer.Replace(key)] = rebindSeed(item, replacer)
		}
		return rebound
	}
	return value
}
