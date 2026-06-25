Fincom Exercise:
Build: go build ./cmd/fincom
Run: ./fincom.exe
API:
  Create Alert: 
   curl.exe -X POST "http://localhost:8080/alerts/create" `
  -H "Content-Type: application/json" `
  -H "X-Tenant-ID: 12345" `
  -d "{\"transaction_id\":\"332211\",\"matched_entity_name\":\"Muhammed\",\"match_score\":50}"

  List Alerts:
  curl.exe -X GET "http://localhost:8080/alerts" `
  -H "Content-Type: application/json" `
  -H "X-Tenant-ID: 12345" `

  Escalate:
  curl.exe -X POST "http://localhost:8080/alerts/{id}/escalate" `
  -H "Content-Type: application/json" `
  -H "X-Tenant-ID: 12345" `

  Decision:
  curl.exe -X POST "http://localhost:8080/alerts/{id}/decision" `
  -H "Content-Type: application/json" `
  -H "X-Tenant-ID: 12345" `
  -d "{\"status\":\"DECIDED\",\"decision_note\":\"Because I said so\"}"


I used in-memory cache like implementation and I used an interface so I can implement it with a DB.
For the emition of an event - I used NATS and implemented it asynchronicly with go routine
For production grade we need to use DEBUG logs, metrics (for Grafana dfashboard) like requests success, type of decisions, errors rate, response time, etc'

Furhter explanation will be given in the code review
