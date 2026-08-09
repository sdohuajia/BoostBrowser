package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"time"
)

type msg struct {
	ID     int            `json:"id"`
	Method string         `json:"method,omitempty"`
	Params any            `json:"params,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func call(c *websocket.Conn, id int, m string, p any) (msg, error) {
	if e := c.WriteJSON(msg{ID: id, Method: m, Params: p}); e != nil {
		return msg{}, e
	}
	for {
		var r msg
		if e := c.ReadJSON(&r); e != nil {
			return r, e
		}
		if r.ID == id {
			return r, nil
		}
	}
}
func main() {
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	r, e := http.Get("http://127.0.0.1:6726/json/version")
	if e != nil {
		panic(e)
	}
	defer r.Body.Close()
	json.NewDecoder(r.Body).Decode(&v)
	h := http.Header{"Origin": []string{"http://127.0.0.1"}}
	b, _, e := websocket.DefaultDialer.Dial(v.WebSocketDebuggerURL, h)
	if e != nil {
		panic(e)
	}
	defer b.Close()
	q, e := call(b, 1, "Target.createTarget", map[string]any{"url": "chrome-extension://ajmpgkcoippjhgcipkhbbajcinebaohc/popup.html"})
	if e != nil {
		panic(e)
	}
	id := q.Result["targetId"].(string)
	p, _, e := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:6726/devtools/page/%s", id), h)
	if e != nil {
		panic(e)
	}
	defer p.Close()
	time.Sleep(time.Second)
	s := `(async()=>{const wait=ms=>new Promise(r=>setTimeout(r,ms));const b=[...document.querySelectorAll('button')].find(x=>/导入钱包|Import Wallet/i.test((x.innerText||'').trim()));if(!b)throw Error('no button');b.click();await wait(500);const c=document.querySelector('[data-testid="onboard-page-import-seed-phrase-or-private-key"]');if(!c)throw Error('no choice');c.click();await wait(700);return {url:location.href,text:(document.body?.innerText||'').slice(0,12000),inputs:[...document.querySelectorAll('input,textarea')].map(x=>({type:x.type,placeholder:x.placeholder,name:x.name,aria:x.getAttribute('aria-label'),testid:x.getAttribute('data-testid')})),buttons:[...document.querySelectorAll('button,[role="button"]')].map(x=>({text:(x.innerText||x.getAttribute('aria-label')||'').trim(),testid:x.getAttribute('data-testid')}))}})()`
	o, e := call(p, 2, "Runtime.evaluate", map[string]any{"expression": s, "awaitPromise": true, "returnByValue": true})
	if e != nil {
		panic(e)
	}
	z, _ := json.Marshal(o.Result)
	fmt.Println(string(z))
}
