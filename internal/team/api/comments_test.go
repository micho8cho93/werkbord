package api_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// The conversation on a ticket, and the notifications its mentions make, over HTTP as the console uses them.

type talk struct {
	t                 *testing.T
	owner, bo, cy     client
	boID, cyID, adaID string
	p, tk             string // the project's and the ticket's addresses
}

func newTalk(t *testing.T) *talk {
	t.Helper()
	ts := newServer(t)
	tk := &talk{t: t, owner: client{t: t, base: ts.URL, token: ownerToken}}
	proj := tk.owner.want(201, "POST", v1+"/projects", `{"name":"Shop"}`)
	tk.p = v1 + "/projects/" + str(proj, "id")
	add := func(name string) (client, string) {
		m := tk.owner.want(201, "POST", v1+"/members", fmt.Sprintf(`{"name":%q}`, name))
		id := str(sub(m, "member"), "id")
		tk.owner.want(204, "PUT", tk.p+"/members/"+id, `{}`)
		return client{t: t, base: ts.URL, token: str(m, "token")}, id
	}
	tk.bo, tk.boID = add("Bo")
	tk.cy, tk.cyID = add("Cy")
	tk.adaID = str(sub(tk.owner.want(200, "GET", v1+"/me", nil), "member"), "id")
	k := tk.owner.want(201, "POST", tk.p+"/tickets", `{"title":"Checkout","status":"available"}`)
	tk.tk = tk.p + "/tickets/" + str(k, "id")
	return tk
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
func mention(id string) map[string]any {
	return map[string]any{"type": "mention", "memberId": id}
}

var postCounter int

func (tk *talk) post(c client, parts ...map[string]any) map[string]any {
	tk.t.Helper()
	postCounter++
	return c.want(201, "POST", tk.tk+"/comments", map[string]any{"clientId": fmt.Sprintf("client-http-%04d", postCounter), "segments": parts})
}

func list(m map[string]any, key string) []map[string]any {
	var out []map[string]any
	raw, _ := m[key].([]any)
	for _, v := range raw {
		out = append(out, v.(map[string]any))
	}
	return out
}

func TestCommentsAndMentionsOverHTTP(t *testing.T) {
	tk := newTalk(t)

	// Ada mentions Bo. Bo is told, Cy is not.
	c := tk.post(tk.owner, text("Bo, please take this: "), mention(tk.boID))
	if str(c, "authorName") != "Ada" || c["version"].(float64) != 1 || c["seq"].(float64) != 1 || c["canEdit"] != true || c["canDelete"] != true {
		t.Fatalf("the comment: %v", c)
	}
	segs := list(c, "segments")
	if len(segs) != 2 || str(segs[1], "type") != "mention" || str(segs[1], "memberId") != tk.boID || str(segs[1], "name") != "Bo" {
		t.Fatalf("segments: %v", segs)
	}
	inbox := tk.bo.want(200, "GET", v1+"/notifications", nil)
	items := list(inbox, "notifications")
	if inbox["unread"].(float64) != 1 || len(items) != 1 || str(items[0], "commentId") != str(c, "id") || str(items[0], "authorName") != "Ada" ||
		str(items[0], "ticketKey") != "WB-1" || str(items[0], "projectName") != "Shop" || str(items[0], "excerpt") != "Bo, please take this: @Bo" {
		t.Fatalf("Bo's notifications: %v", inbox)
	}
	if got := tk.cy.want(200, "GET", v1+"/notifications", nil); got["unread"].(float64) != 0 || len(list(got, "notifications")) != 0 {
		t.Fatalf("Cy's: %v", got)
	}
	// The overview carries the same count, in total and by project.
	ov := tk.bo.want(200, "GET", v1+"/overview", nil)
	if ov["mentions"].(float64) != 1 || list(ov, "projects")[0]["mentions"].(float64) != 1 {
		t.Fatalf("overview: %v", ov)
	}

	// Opening the comment: the ticket's comments around it, with it named as the focus. Reading them does not read the notification.
	page := tk.bo.want(200, "GET", tk.tk+"/comments?around="+str(c, "id"), nil)
	if str(page, "focus") != str(c, "id") || page["unreadMentions"].(float64) != 1 || page["canPost"] != true || page["readOnly"] != false {
		t.Fatalf("around: %v", page)
	}
	first := list(page, "comments")[0]
	if first["mentionsMe"] != true || sub(first, "notification")["read"] != false {
		t.Fatalf("the highlighted comment: %v", first)
	}
	if tk.bo.want(200, "GET", v1+"/notifications", nil)["unread"].(float64) != 1 {
		t.Fatal("opening read it")
	}

	// Marking it read: idempotent, personal.
	nid := str(sub(first, "notification"), "id")
	tk.cy.want(404, "POST", v1+"/notifications/"+nid+"/read", nil)
	read := tk.bo.want(200, "POST", v1+"/notifications/"+nid+"/read", nil)
	again := tk.bo.want(200, "POST", v1+"/notifications/"+nid+"/read", nil)
	if read["readAt"] == nil || read["readAt"] != again["readAt"] {
		t.Fatalf("reading twice: %v / %v", read, again)
	}
	tk.bo.want(404, "POST", v1+"/notifications/tnt_nothing/read", nil)
	if tk.bo.want(200, "GET", v1+"/notifications", nil)["unread"].(float64) != 0 {
		t.Fatal("still unread")
	}
	if all := tk.bo.want(200, "GET", v1+"/notifications?state=all", nil); len(list(all, "notifications")) != 1 {
		t.Fatalf("all: %v", all)
	}
	tk.bo.want(400, "GET", v1+"/notifications?state=bogus", nil)
	tk.bo.want(400, "GET", v1+"/notifications?limit=x", nil)
	tk.bo.want(400, "GET", v1+"/notifications?cursor=garbage", nil)
	tk.bo.want(404, "GET", v1+"/notifications?projectId=tpj_nothing", nil)
	if p := tk.bo.want(200, "GET", v1+"/notifications?projectId="+strings.TrimPrefix(tk.p, v1+"/projects/")+"&state=all", nil); len(list(p, "notifications")) != 1 {
		t.Fatalf("project filter: %v", p)
	}

	// Editing needs the version, and only the author edits; a conflict says so.
	cp := tk.tk + "/comments/" + str(c, "id")
	tk.owner.want(400, "PATCH", cp, map[string]any{"segments": []any{text("x")}}) // no version
	tk.bo.want(403, "PATCH", cp, map[string]any{"version": 1, "segments": []any{text("hijacked")}})
	tk.owner.want(409, "PATCH", cp, map[string]any{"version": 7, "segments": []any{text("stale")}})
	edited := tk.owner.want(200, "PATCH", cp, map[string]any{"version": 1, "segments": []any{text("Bo, please take this: "), mention(tk.boID), text(" today")}})
	if edited["version"].(float64) != 2 || edited["editedAt"] == nil {
		t.Fatalf("edited: %v", edited)
	}
	tk.owner.want(409, "PATCH", cp, map[string]any{"version": 1, "segments": []any{text("again, with the old version")}})
	tk.owner.want(400, "PATCH", cp, `{"version":2,"segments":[{"type":"text","text":"x"}],"bogus":1}`)
	tk.owner.want(400, "PATCH", cp, map[string]any{"version": 2, "segments": []any{}})

	// Deleting: a tombstone, whoever it was; a member may not delete another's; deleting twice is the same answer.
	tk.cy.want(403, "DELETE", cp, nil)
	tk.owner.want(409, "DELETE", cp+"?version=1", nil)
	tk.owner.want(400, "DELETE", cp+"?version=x", nil)
	gone := tk.owner.want(200, "DELETE", cp+"?version=2", nil)
	if gone["deletedAt"] == nil || str(gone, "deletedBy") != "author" || len(list(gone, "segments")) != 0 {
		t.Fatalf("tombstone: %v", gone)
	}
	if twice := tk.owner.want(200, "DELETE", cp, nil); twice["deletedAt"] != gone["deletedAt"] {
		t.Fatalf("deleting twice: %v", twice)
	}
	tk.owner.want(409, "PATCH", cp, map[string]any{"version": 3, "segments": []any{text("back")}})
	if got := list(tk.bo.want(200, "GET", tk.tk+"/comments", nil), "comments"); len(got) != 1 || got[0]["deletedAt"] == nil || got[0]["mentionsMe"] == true {
		t.Fatalf("the thread after deleting: %v", got)
	}
	if tk.bo.want(200, "GET", v1+"/notifications", nil)["unread"].(float64) != 0 {
		t.Fatal("deleting left a notification")
	}
}

func TestPostingTwiceOverHTTPGivesTheFirstCommentBack(t *testing.T) {
	tk := newTalk(t)
	body := map[string]any{"clientId": "client-retry-http", "segments": []any{text("hello "), mention(tk.cyID)}}
	first := tk.owner.want(201, "POST", tk.tk+"/comments", body)
	for i := 0; i < 2; i++ {
		again := tk.owner.want(200, "POST", tk.tk+"/comments", body) // 200, not 201: nothing was made
		if str(again, "id") != str(first, "id") {
			t.Fatalf("a retry made another comment: %v", again)
		}
	}
	other := map[string]any{"clientId": "client-retry-http", "segments": []any{text("something else")}}
	tk.owner.want(409, "POST", tk.tk+"/comments", other)
	tk.owner.want(400, "POST", tk.tk+"/comments", map[string]any{"clientId": "x", "segments": []any{text("hi")}})
	tk.owner.want(400, "POST", tk.tk+"/comments", map[string]any{"segments": []any{text("hi")}})
	if got := tk.cy.want(200, "GET", v1+"/notifications", nil); got["unread"].(float64) != 1 {
		t.Fatalf("Cy was told %v times", got["unread"])
	}
}

func TestOnlyPeopleOnTheProjectUseItsComments(t *testing.T) {
	tk := newTalk(t)
	c := tk.post(tk.bo, text("hello"))
	eve := tk.owner.want(201, "POST", v1+"/members", `{"name":"Eve"}`)
	eveC := client{t: t, base: tk.owner.base, token: str(eve, "token")}
	eveID := str(sub(eve, "member"), "id")
	cp := tk.tk + "/comments"
	eveC.want(404, "GET", cp, nil)
	eveC.want(404, "POST", cp, map[string]any{"clientId": "client-eve-0001", "segments": []any{text("hi")}})
	eveC.want(404, "PATCH", cp+"/"+str(c, "id"), map[string]any{"version": 1, "segments": []any{text("hi")}})
	eveC.want(404, "DELETE", cp+"/"+str(c, "id"), nil)
	// She cannot be mentioned until she is on the project, and no token at all reads nothing.
	tk.bo.want(400, "POST", cp, map[string]any{"clientId": "client-bo-000001", "segments": []any{text("cc "), mention(eveID)}})
	anon := client{t: t, base: tk.owner.base}
	anon.want(401, "GET", cp, nil)
	anon.want(401, "GET", v1+"/notifications", nil)
	// An address of a ticket that is not there, or a comment that is not on it.
	tk.bo.want(404, "GET", tk.p+"/tickets/ttk_nothing/comments", nil)
	tk.bo.want(404, "PATCH", cp+"/tcm_nothing", map[string]any{"version": 1, "segments": []any{text("hi")}})
	tk.bo.want(404, "GET", cp+"?around=tcm_nothing", nil)
}

func TestCommentsArePagedOverHTTP(t *testing.T) {
	tk := newTalk(t)
	for i := 1; i <= 12; i++ {
		tk.post(tk.owner, text(fmt.Sprintf("message %d", i)))
	}
	latest := tk.bo.want(200, "GET", tk.tk+"/comments?limit=5", nil)
	got := list(latest, "comments")
	if len(got) != 5 || str(got[4], "authorName") != "Ada" || latest["hasOlder"] != true || latest["hasNewer"] != false || latest["total"].(float64) != 12 || latest["olderBefore"].(float64) != 8 {
		t.Fatalf("latest: %v", latest)
	}
	older := tk.bo.want(200, "GET", fmt.Sprintf("%s/comments?limit=5&before=%d", tk.tk, int(latest["olderBefore"].(float64))), nil)
	if o := list(older, "comments"); len(o) != 5 || o[0]["seq"].(float64) != 3 || o[4]["seq"].(float64) != 7 || older["hasNewer"] != true {
		t.Fatalf("older: %v", older)
	}
	// after=0 is the start of the conversation, not "no cursor".
	if start := tk.bo.want(200, "GET", tk.tk+"/comments?limit=5&after=0", nil); list(start, "comments")[0]["seq"].(float64) != 1 || start["hasOlder"] != false || start["hasNewer"] != true {
		t.Fatalf("after=0: %v", start)
	}
	tk.bo.want(400, "GET", tk.tk+"/comments?before=3&after=2", nil)
	tk.bo.want(400, "GET", tk.tk+"/comments?before=x", nil)
	tk.bo.want(400, "GET", tk.tk+"/comments?limit=-1", nil)
}

// A comment may be 20,000 characters, which is more than the 64 KiB any other request may be once they are four bytes each or
// written by an encoder that escapes everything; a body beyond what a comment can need is still refused.
func TestACommentCanBeAsLongAsAllowedAndNoLonger(t *testing.T) {
	tk := newTalk(t)
	long := strings.Repeat("🙂", 20000) // 80,000 bytes, more than any other request may be
	tk.owner.want(201, "POST", tk.tk+"/comments", map[string]any{"clientId": "client-long-0001", "segments": []any{text(long)}})
	// Written by an encoder that escapes every character: twelve bytes each.
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString(`🙂`)
	}
	tk.owner.want(201, "POST", tk.tk+"/comments", `{"clientId":"client-long-0002","segments":[{"type":"text","text":"`+b.String()+`"}]}`)
	tk.owner.want(400, "POST", tk.tk+"/comments", map[string]any{"clientId": "client-long-0003", "segments": []any{text(long + "x")}})
	tk.owner.want(400, "POST", tk.tk+"/comments", `{"clientId":"client-long-0004","segments":[{"type":"text","text":"`+strings.Repeat("x", 520<<10)+`"}]}`)
	// Other requests keep their small limit.
	tk.owner.want(400, "POST", tk.p+"/tickets", `{"title":"x","description":"`+strings.Repeat("x", 70<<10)+`"}`)
	// Many mentions of the same person are one recipient; fifty-one different people are too many.
	var parts []any
	for i := 0; i < 100; i++ {
		parts = append(parts, mention(tk.boID), text(" "))
	}
	tk.owner.want(201, "POST", tk.tk+"/comments", map[string]any{"clientId": "client-many-0001", "segments": parts})
}

func TestMarkupInACommentIsNeverReturnedAsMarkup(t *testing.T) {
	tk := newTalk(t)
	const evil = `<script>alert(1)</script><img src=x onerror=alert(1)>`
	c := tk.post(tk.owner, text(evil))
	_, _, raw := tk.bo.do("GET", tk.tk+"/comments", nil)
	// It is JSON, escaped as JSON is: no raw angle bracket reaches a client that mishandles it as HTML.
	if strings.Contains(string(raw), "<script>") || strings.Contains(string(raw), "<img") {
		t.Fatalf("raw markup in the response: %s", raw)
	}
	var back struct {
		Comments []struct {
			Segments []struct{ Text string } `json:"segments"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(raw, &back); err != nil || back.Comments[0].Segments[0].Text != evil {
		t.Fatalf("what was written changed: %v %v", back, err)
	}
	tk.owner.want(400, "POST", tk.tk+"/comments", `{"clientId":"client-evil-0001","segments":[{"type":"html","text":"<b>x</b>"}]}`)
	tk.owner.want(400, "POST", tk.tk+"/comments", `{"clientId":"client-evil-0002","segments":[{"type":"text","text":"a","html":"<b>x</b>"}]}`)
	_ = c
}
