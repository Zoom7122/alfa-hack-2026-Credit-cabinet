// field-catalog inventories metadata from the real collector against synthetic
// mock-api scenarios. It never stores source values, credentials or identifiers.
package main

import (
 "context"
 "encoding/json"
 "flag"
 "fmt"
 "os"
 "regexp"
 "sort"
 "strings"
 "time"

 "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/mockapi"
 "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

type field struct{System string `json:"system"`;Path string `json:"path"`;Type string `json:"type"`}
var dynamic=regexp.MustCompile(`^\d{9}$|^\d{2}:\d{2}:\d+:\d+$|^\d{10,}$|^XTC|^JCB|^ACC-`)
func main(){
 url:=flag.String("mock-api","http://127.0.0.1:19090","An isolated synthetic mock-api instance");flag.Parse()
 if !strings.HasPrefix(*url,"http://127.0.0.1:")&&!strings.HasPrefix(*url,"http://localhost:"){fmt.Fprintln(os.Stderr,"use an isolated loopback mock-api instance");os.Exit(1)}
 client:=mockapi.New(*url,3*time.Second,"044525999");fields:=map[string]field{}
 var walk func(string,string,any)
 walk=func(system,path string,value any){
  typ:="UNKNOWN";switch value.(type){case map[string]any:typ="OBJECT";case []any:typ="ARRAY";case string:typ="STRING";case bool:typ="BOOLEAN";case float64:typ="NUMBER"}
  if path!=""{key:=system+":"+path;if old,ok:=fields[key];!ok||old.Type=="UNKNOWN"{fields[key]=field{system,path,typ}}}
  switch v:=value.(type){case map[string]any:for k,child:=range v{if dynamic.MatchString(k){k="*"};next:=k;if path!=""{next=path+"."+k};walk(system,next,child)};case []any:for _,child:=range v{walk(system,path+"[]",child)}}
 }
 fixtures:=[]struct{inn,director,poa string}{{"7799000012","779900010139","0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37"},{"7799000020","779900020136","3c9e7d10-55a2-4f61-8b0e-2a7c4f9d6b12"},{"7799000037","779900030147","a71b4e02-9d3c-4c88-b5f0-81e6c2d3a945"},{"7799000044","779900040158",""},{"779900010202","779900010202",""}}
 seen:=map[string]bool{}
 for i,f:=range fixtures{
  consent:=&domain.Consent{ID:"catalog-synthetic",ValidUntil:"2030-12-31",Scope:[]string{domain.TaxScopeDeclarations,domain.TaxScopeENSBalance}}
  input:=domain.CollectInput{INN:f.inn,Director:&domain.Person{INN:f.director,LastName:"Тест",FirstName:"Тест",BirthDate:"1984-07-19"},BKICompanyConsent:consent,BKIPersonConsent:consent,TaxConsent:consent,OtherBankBIKs:[]string{"044525225","044525593"},MarketplaceTokens:map[string]string{"wildberries":"synthetic-token","ozon":"synthetic-token"},OFDToken:"synthetic-token",Collateral:domain.Collateral{CadastralNumbers:[]string{"50:21:0110203:845","50:21:0110203:412"},VINs:[]string{"XTC652005N1498213"},Serials:[]string{"JCB3CX2023118457"}},POANumber:f.poa,RepresentativeINN:"779900010333"}
  for _,result:=range client.CollectAll(context.Background(),fmt.Sprintf("catalog-%d",i),input){
   if len(result.Data)==0{continue};var v any;if json.Unmarshal(result.Data,&v)!=nil{continue};seen[string(result.System)]=true;walk(string(result.System),"",v)
  }
 }
 if len(seen)!=len(domain.Systems){fmt.Fprintf(os.Stderr,"only %d/%d systems returned metadata\n",len(seen),len(domain.Systems));os.Exit(1)}
 result:=make([]field,0,len(fields));for _,f:=range fields{result=append(result,f)};sort.Slice(result,func(i,j int)bool{if result[i].System==result[j].System{return result[i].Path<result[j].Path};return result[i].System<result[j].System})
 encoder:=json.NewEncoder(os.Stdout);encoder.SetIndent("","  ");if err:=encoder.Encode(result);err!=nil{panic(err)}
}
