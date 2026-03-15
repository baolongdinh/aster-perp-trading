# Auth v1 – Examples

**Query string:** totalParams = sorted key=value (query + body, no `&` between if both). Sign: `echo -n "<totalParams>" | openssl dgst -sha256 -hmac "SECRET"`. Send: query + `&signature=<hex>` or body + `&signature=<hex>`. Header: X-MBX-APIKEY.

**POST /fapi/v1/order (query):** e.g. `?symbol=BTCUSDT&side=BUY&type=LIMIT&timeInForce=GTC&quantity=1&price=9000&recvWindow=5000&timestamp=...&signature=...`  
**Body:** same key=value string as body, signature last. **Mixed:** totalParams = query concat body (no `&` between); sign that string.

**Timing:** Accept if timestamp < serverTime+1000 and serverTime-timestamp ≤ recvWindow. recvWindow ≤ 5000.
