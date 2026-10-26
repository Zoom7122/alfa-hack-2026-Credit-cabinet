package mockkit_test

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

func TestCabinetSeedsRebindIdentityWithoutChangingFinancialNumbers(t *testing.T) {
	const original = `{"inn":"7799000012","director":{"inn":"779900010139","name":"Ковалёв Игорь Сергеевич","first_name":"Игорь"},"date":"14.03.2021","amount":9007199254740993,"zero":0,"absent":null}`
	fs := fstest.MapFS{"seed/7799000012.json": {Data: []byte(original)}}
	seeds := mockkit.CabinetSeeds(fs, "seed")
	if string(seeds["7799000012"]) != original {
		t.Fatal("source fixture changed")
	}
	var got struct {
		INN      string `json:"inn"`
		Director struct {
			INN       string `json:"inn"`
			Name      string `json:"name"`
			FirstName string `json:"first_name"`
		}
		Date   string `json:"date"`
		Amount int64  `json:"amount"`
	}
	if err := json.Unmarshal(seeds["7701234567"], &got); err != nil {
		t.Fatal(err)
	}
	if got.INN != "7701234567" || got.Director.INN != "770112345695" || got.Director.Name != "Петров Пётр Петрович" || got.Director.FirstName != "Пётр" || got.Date != "14.03.2019" || got.Amount != 9007199254740993 {
		t.Fatalf("rebound fixture: %+v", got)
	}
	if !strings.Contains(string(seeds["7701234567"]), `"zero":0`) || !strings.Contains(string(seeds["7701234567"]), `"absent":null`) {
		t.Fatal("zero and missing values lost their distinction")
	}
	if _, ok := seeds["7799000999"]; ok {
		t.Fatal("unknown company received a profile")
	}
	fs["seed/7701234567.json"] = &fstest.MapFile{Data: []byte(`{"explicit":true}`)}
	if string(mockkit.CabinetSeeds(fs, "seed")["7701234567"]) != `{"explicit":true}` {
		t.Fatal("explicit fixture was overwritten")
	}
}
