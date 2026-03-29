curl -X POST http://localhost:8080/api/tx/deploy \
  -F "contract_name=MyNewContract" \
  -F "file=@./contracts/example_asset/my_contract.wasm"



  curl -X POST http://localhost:8080/api/tx/submit \
-H "Content-Type: application/json" \
-d '{
    "tx_id": "TX_REAL_002",
    "contract_name": "MyNewContract",
    "function_name": "verify_tx",
    "payload": "eyAiaWQiOiAiQTEiLCAiY29sb3IiOiAicmVkIiwgImFjdGlvbiI6ICJjcmVhdGUiIH0="
}'

 "payload": {
        "id": "A1",
        "color": "blue",
        "action": "create"
    }


curl -X POST http://localhost:8080/api/tx/submit \
-H "Content-Type: application/json" \
-d '{
    "tx_id": "TX_HACK_999",
    "contract_name": "MyNewContract",
    "function_name": "verify_tx",
    "payload": "eyAiaWQiOiAiWDk5IiwgImFjdGlvbiI6ICJoYWNrIiB9"
}'

"payload": {
        "id": "X99",
        "action": "hack"
    }


curl -X GET "http://localhost:8080/api/state?key=Asset_A2"