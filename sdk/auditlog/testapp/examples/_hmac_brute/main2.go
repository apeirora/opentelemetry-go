package main
import ("crypto/sha256";"encoding/hex";"encoding/json";"fmt";"sort";"time")
const want="d2114a617e253982176a1902e61b6267c4b97f0afc3d5a73e8c8acf89d72889"
const body=`{"event":"user.login","n":0,"id":"rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a"}`
type a struct{K,V string `json:"key,omitempty"`; Value string `json:"value,omitempty"`}
func main(){
  ts,_:=time.Parse(time.RFC3339Nano,"2026-05-19T12:36:04.2396044Z")
  tsStr:=ts.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
  attrs:=[]map[string]string{{"key":"audit.record_id","value":"rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a"},{"key":"base","value":"testapp"},{"key":"audit.actor","value":"alice@example.com"},{"key":"audit.actor_type","value":"user"},{"key":"audit.action","value":"login"},{"key":"audit.resource","value":"/api/widgets"},{"key":"audit.outcome","value":"success"},{"key":"audit.schema_version","value":"1.0"},{"key":"audit.source_ip","value":"192.0.2.10"},{"key":"sig_content:meta","value":"meta"}}
  lr:=map[string]any{"timeUnixNano":"1779194164239604400","observedTimeUnixNano":"1779194164239604400","severityNumber":9,"severityText":"INFO","eventName":"user.login","body":map[string]string{"stringValue":body},"attributes":attrs}
  b,_:=json.Marshal(lr)
  sum:=sha256.Sum256(b)
  fmt.Println("sha256 logrecord json", hex.EncodeToString(sum[:]))
  p:=map[string]any{"timestamp":tsStr,"observed_timestamp":tsStr,"event_name":"user.login","actor":"alice@example.com","actor_type":"user","action":"login","resource":"/api/widgets","outcome":"success","source_ip":"192.0.2.10","body":body,"record_id":"rec-e4c39188-a682-4dc2-a17b-9e5ba0ab7a0a","schema_version":"1.0","attributes":attrs}
  b2,_:=json.Marshal(p)
  sum2:=sha256.Sum256(b2)
  fmt.Println("sha256 no attrs struct", hex.EncodeToString(sum2[:]))
  fmt.Println("want", want)
}
