package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func everyone(names map[string]string) func(string) (string, bool) {
	return func(id string) (string, bool) { n, ok := names[id]; return n, ok }
}

func text(s string) Segment     { return Segment{Type: SegmentText, Text: s} }
func mention(id string) Segment { return Segment{Type: SegmentMention, MemberID: id} }
func isInvalid(err error) bool  { return errors.Is(err, ErrInvalid) }
func texts(segs []Segment) []string {
	var out []string
	for _, s := range segs {
		if s.Type == SegmentMention {
			out = append(out, "@"+s.MemberID+"="+s.Name)
		} else {
			out = append(out, s.Text)
		}
	}
	return out
}

func TestACommentIsCleanedIntoOrderedTextAndMentions(t *testing.T) {
	names := map[string]string{"tmb_bo": "Bo", "tmb_cy": "Cy"}
	segs, people, err := CleanComment([]Segment{text("  Hi "), text("there "), mention("tmb_bo"), text(""), text(" and\r\n"), mention("tmb_cy"), mention("tmb_bo"), text("\r  \n")}, everyone(names))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(texts(segs), "|")
	if want := "Hi there |@tmb_bo=Bo| and\n|@tmb_cy=Cy|@tmb_bo=Bo"; got != want {
		t.Fatalf("segments %q, want %q", got, want)
	}
	if fmt.Sprint(people) != "[tmb_bo tmb_cy]" {
		t.Fatalf("people %v: each person once, in the order they first appear", people)
	}
	if fmt.Sprint(MentionedIDs(segs)) != "[tmb_bo tmb_cy]" {
		t.Fatalf("MentionedIDs %v", MentionedIDs(segs))
	}
}

func TestAMentionIsBoundToTheMemberAndNeverToANameAClientSends(t *testing.T) {
	segs, _, err := CleanComment([]Segment{{Type: SegmentMention, MemberID: "tmb_bo", Name: "Mallory"}}, everyone(map[string]string{"tmb_bo": "Bo"}))
	if err != nil {
		t.Fatal(err)
	}
	if segs[0].Name != "Bo" || segs[0].MemberID != "tmb_bo" {
		t.Fatalf("the name came from the client: %+v", segs[0])
	}
	// Somebody who cannot be mentioned (not on the project, not in the workspace, no id) is refused.
	for _, id := range []string{"tmb_nobody", "", "  "} {
		if _, _, err := CleanComment([]Segment{text("hi"), mention(id)}, everyone(map[string]string{"tmb_bo": "Bo"})); !isInvalid(err) {
			t.Errorf("a mention of %q was accepted: %v", id, err)
		}
	}
}

func TestACommentThatIsEmptyOrNotTextIsRefused(t *testing.T) {
	resolve := everyone(map[string]string{"tmb_bo": "Bo"})
	for name, in := range map[string][]Segment{
		"nothing":           nil,
		"only space":        {text("  \n\t ")},
		"only empty":        {text(""), text("")},
		"unknown kind":      {{Type: "html", Text: "<b>x</b>"}},
		"a text with an id": {{Type: SegmentText, Text: "x", MemberID: "tmb_bo"}},
		"a mention + text":  {{Type: SegmentMention, MemberID: "tmb_bo", Text: "x"}},
		"a control char":    {text("a\x00b")},
		"an escape":         {text("a\x1b[31mred")},
		"bidi override":     {text("evil‮txt.exe")},
		"bidi isolate":      {text("x⁦y")},
		"not utf-8":         {text("a\xffb")},
	} {
		if _, _, err := CleanComment(in, resolve); !isInvalid(err) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	// A mention alone is a comment; tabs and line breaks, emoji and a joiner are text.
	if _, _, err := CleanComment([]Segment{mention("tmb_bo")}, resolve); err != nil {
		t.Errorf("a mention alone: %v", err)
	}
	if segs, _, err := CleanComment([]Segment{text("a\tb\n\nc 👩‍💻 é")}, resolve); err != nil || segs[0].Text != "a\tb\n\nc 👩‍💻 é" {
		t.Errorf("ordinary text was changed or refused: %v %v", segs, err)
	}
}

func TestMarkupIsKeptAsTextAndNeverInterpreted(t *testing.T) {
	const html = `<img src=x onerror=alert(1)> <script>alert(1)</script> &amp; [link](javascript:alert(1)) **bold**`
	segs, _, err := CleanComment([]Segment{text(html)}, everyone(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Type != SegmentText || segs[0].Text != html {
		t.Fatalf("markup was changed: %+v", segs)
	}
	// What is stored and read back is the same text.
	back, err := ParseSegments(EncodeSegments(segs))
	if err != nil || len(back) != 1 || back[0].Text != html {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

func TestCommentLimits(t *testing.T) {
	resolve := everyone(map[string]string{"tmb_bo": "Bo"})
	if _, _, err := CleanComment([]Segment{text(strings.Repeat("é", MaxCommentLen))}, resolve); err != nil {
		t.Errorf("a comment of exactly %d characters: %v", MaxCommentLen, err)
	}
	if _, _, err := CleanComment([]Segment{text(strings.Repeat("é", MaxCommentLen+1))}, resolve); !isInvalid(err) {
		t.Errorf("a comment one character too long: %v", err)
	}
	// A mention counts as it is read: "@Bo" is three characters.
	if _, _, err := CleanComment([]Segment{text(strings.Repeat("x", MaxCommentLen-3)), mention("tmb_bo")}, resolve); err != nil {
		t.Errorf("a comment that just fits with a mention: %v", err)
	}
	if _, _, err := CleanComment([]Segment{text(strings.Repeat("x", MaxCommentLen-2)), mention("tmb_bo")}, resolve); !isInvalid(err) {
		t.Errorf("a comment that is too long because of its mention: %v", err)
	}
	// Fifty different people, not fifty-one; the same person many times is one.
	names := map[string]string{}
	var fifty, fiftyOne []Segment
	for i := 0; i <= MaxCommentMentions; i++ {
		id := fmt.Sprintf("tmb_%02d", i)
		names[id] = fmt.Sprintf("P%02d", i)
		if i < MaxCommentMentions {
			fifty = append(fifty, mention(id), text(" "))
		}
		fiftyOne = append(fiftyOne, mention(id), text(" "))
	}
	if _, people, err := CleanComment(fifty, everyone(names)); err != nil || len(people) != MaxCommentMentions {
		t.Errorf("fifty people: %d, %v", len(people), err)
	}
	if _, _, err := CleanComment(fiftyOne, everyone(names)); !isInvalid(err) {
		t.Errorf("fifty-one people: %v", err)
	}
	var again []Segment
	for i := 0; i < 100; i++ {
		again = append(again, mention("tmb_bo"), text(" "))
	}
	if _, people, err := CleanComment(again, resolve); err != nil || len(people) != 1 {
		t.Errorf("one person a hundred times: %v %v", people, err)
	}
	var many []Segment
	for i := 0; i <= MaxCommentSegments; i++ {
		many = append(many, mention("tmb_bo"))
	}
	if _, _, err := CleanComment(many, resolve); !isInvalid(err) {
		t.Errorf("more than %d parts: %v", MaxCommentSegments, err)
	}
}

func TestTwoRequestsToPostTheSameWordsHaveTheSameDigest(t *testing.T) {
	a, _, _ := CleanComment([]Segment{text("see "), mention("tmb_bo")}, everyone(map[string]string{"tmb_bo": "Bo"}))
	b, _, _ := CleanComment([]Segment{text("see "), {Type: SegmentMention, MemberID: "tmb_bo", Name: "Whoever"}}, everyone(map[string]string{"tmb_bo": "Bobby"}))
	c, _, _ := CleanComment([]Segment{text("see "), mention("tmb_cy")}, everyone(map[string]string{"tmb_cy": "Cy"}))
	d, _, _ := CleanComment([]Segment{text("see me")}, everyone(nil))
	if SegmentsDigest(a) != SegmentsDigest(b) || !SameSegments(a, b) {
		t.Error("the names a comment was shown with changed what it says")
	}
	if SameSegments(a, c) || SameSegments(a, d) || SameSegments(c, d) {
		t.Error("different words or people have the same digest")
	}
}

func TestAnExcerptIsOneBoundedLineWithMentionsAsNames(t *testing.T) {
	segs, _, _ := CleanComment([]Segment{text("Can you\n\n look  at "), mention("tmb_bo"), text("'s change?")}, everyone(map[string]string{"tmb_bo": "Bo"}))
	if got := PlainText(segs); got != "Can you look at @Bo's change?" {
		t.Errorf("plain text %q", got)
	}
	if got := Excerpt(segs, 160); got != "Can you look at @Bo's change?" {
		t.Errorf("excerpt %q", got)
	}
	long, _, _ := CleanComment([]Segment{text(strings.Repeat("word ", 100))}, everyone(nil))
	got := Excerpt(long, 20)
	if r := []rune(got); len(r) != 20 || r[19] != '…' || strings.HasSuffix(string(r[:19]), " ") {
		t.Errorf("a long excerpt %q (%d characters)", got, len([]rune(got)))
	}
}

func TestACreationIdIsAnOpaqueTokenOfAKnownShape(t *testing.T) {
	for _, ok := range []string{"abcd1234", "c_1700000000000-ab12", strings.Repeat("x", 64)} {
		if _, err := CleanClientID(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "short", strings.Repeat("x", 65), "has space 1234", "semi;colon-1", "új-azonosító-1"} {
		if _, err := CleanClientID(bad); !isInvalid(err) {
			t.Errorf("%q was accepted (%v)", bad, err)
		}
	}
}

func TestOnlyAnUnreadAndNotWithdrawnNotificationWaits(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		n    Notification
		want bool
	}{{Notification{}, true}, {Notification{ReadAt: &now}, false}, {Notification{Withdrawn: true}, false}} {
		if c.n.Unread() != c.want {
			t.Errorf("%+v unread = %v", c.n, c.n.Unread())
		}
	}
}

func TestProjectRolesAndComments(t *testing.T) {
	for _, r := range ProjectRoles() {
		if !r.Can(PPCommentsPost) {
			t.Errorf("%s cannot post comments", r)
		}
		if r.Can(PPCommentsModerate) != (r == ProjectOwner) {
			t.Errorf("%s may moderate comments = %v: only a project's owner does", r, r.Can(PPCommentsModerate))
		}
	}
	// A role that may only look (none exists today; an unknown one is the same) never writes.
	if ProjectRole("viewer").Can(PPCommentsPost) || ProjectRole("").Can(PPCommentsPost) {
		t.Error("a role that does not exist may post comments")
	}
}
