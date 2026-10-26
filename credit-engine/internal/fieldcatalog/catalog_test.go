package fieldcatalog

import (
 "os"
 "strings"
 "testing"

 "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
 "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
)

func baseline(t *testing.T,name string) model.ModelDefinition{t.Helper();data,err:=os.Open("../../models/"+name+"-v2.json");if err!=nil{t.Fatal(err)};defer data.Close();d,err:=model.Decode(data);if err!=nil{t.Fatal(err)};return d}
func lookup(t *testing.T,c Catalog,system,path string) Field{t.Helper();for _,f:=range c.Fields{if f.System==system&&f.Path==path{return f}};t.Fatal("missing metadata",system,path);return Field{}}
func has(in []string,want string)bool{for _,s:=range in{if s==want{return true}};return false}

func TestCatalogCoversSystemsAndKnownBindings(t *testing.T){
 c,err:=Read();if err!=nil{t.Fatal(err)};if len(c.Systems)!=19||len(c.Fields)<1000{t.Fatal("incomplete collector inventory")}
 known:=map[model.Source]bool{};for _,name:=range []string{"ip","legal"}{d:=baseline(t,name);bindings,err:=sourcecatalog.Bindings(d.BorrowerType);if err!=nil{t.Fatal(err)};for _,b:=range bindings{known[b.Source]=true}}
 systems,ids:=map[string]bool{},map[string]bool{};for _,f:=range c.Fields{if ids[f.ID]||f.Title==""||f.Type==""||strings.Contains(f.Path,"779900"){t.Fatal("invalid metadata",f.ID)};ids[f.ID]=true;systems[f.System]=true;for _,binding:=range f.Bindings{if !known[binding]{t.Fatal("unknown scoring provenance",f.ID,binding)}}};if len(systems)!=19{t.Fatal("missing sources",systems)}
}

func TestUsageTracksComputedScoresAndZeroPointControls(t *testing.T){
 d:=baseline(t,"ip");c,err:=Analyze(d);if err!=nil{t.Fatal(err)}
 flow:=lookup(t,c,"CORE_BANKING","turnover.months[].credit_turnover");if !flow.Used||!flow.Derived||!has(flow.Roles,"SCORE"){t.Fatal("indirect economic inputs misclassified",flow)}
 active:=lookup(t,c,"FNS","pb.vyp.sulst_ex");if !active.Used||has(active.Roles,"SCORE")||!has(active.Roles,"STOP"){t.Fatal("zero-point stop misclassified",active)}
 extra:=lookup(t,c,"MARKETPLACES","wildberries.profile.rating");if extra.Used||extra.Reason!="Не передаётся в скоринговые признаки"{t.Fatal(extra)}
 for i:=range d.Criteria{if d.Criteria[i].ID=="IP_Q09"{d.Criteria[i].Enabled=false}}
 c,err=Analyze(d);if err!=nil{t.Fatal(err)};relationship:=lookup(t,c,"CORE_BANKING","client.client_since");if has(relationship.Roles,"SCORE")||!relationship.Used||!has(relationship.Roles,"COMPLETENESS"){t.Fatal("disabling points disabled completeness",relationship)}
 for i:=range d.Parameters{if d.Parameters[i].ID=="bank_relationship_months"{d.Parameters[i].Required=false}}
 c,err=Analyze(d);if err!=nil{t.Fatal(err)};relationship=lookup(t,c,"CORE_BANKING","client.client_since");if relationship.Used||relationship.Reason!="Балльный критерий выключен"{t.Fatal(relationship)}
}
