package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Comments are the shared conversation on a ticket. Anyone who may see the ticket reads them; anyone with
// comments.post writes them. A comment mentions people, never addresses: its content is an ordered list of
// plain-text and member-mention segments, so nothing in it is ever interpreted as markup, and a mention is
// bound to a member's ID and not to the name they had when it was written. Only a mention makes a
// Notification, and a notification belongs to the one person it was made for.

// More ID prefixes.
const (
	PrefixComment      = "tcm"
	PrefixNotification = "tnt"
)

// Limits on a comment.
const (
	// MaxCommentLen is the most characters a comment may have as its author sees it: the text, and each mention
	// as "@" and the name.
	MaxCommentLen = 20000
	// MaxCommentMentions is the most distinct people one comment may mention.
	MaxCommentMentions = 50
	// MaxCommentSegments bounds how many parts a comment is made of, whatever its length.
	MaxCommentSegments = 500
	// DefaultCommentPage and MaxCommentPage bound how many comments one read returns.
	DefaultCommentPage = 50
	MaxCommentPage     = 100
	// DefaultNotificationPage and MaxNotificationPage bound how many notifications one read returns.
	DefaultNotificationPage = 50
	MaxNotificationPage     = 100
	// ExcerptLen is how long a notification's quotation of the comment is.
	ExcerptLen = 160
)

// SegmentKind is what a part of a comment is.
type SegmentKind string

const (
	// SegmentText is words, kept exactly as written (line breaks included).
	SegmentText SegmentKind = "text"
	// SegmentMention is a person, by the ID they have for as long as they are in the workspace.
	SegmentMention SegmentKind = "mention"
)

// Segment is one part of a comment's content: text, or a mention of a member. Name is only ever shown, never
// trusted or used to find the person: what a client sends is ignored, and what is stored is the name the person
// had when they were mentioned, which stands in for it if they have left.
type Segment struct {
	Type     SegmentKind `json:"type"`
	Text     string      `json:"text,omitempty"`
	MemberID string      `json:"memberId,omitempty"`
	Name     string      `json:"name,omitempty"`
}

// Who deleted a comment, as a tombstone says it.
const (
	DeletedByAuthor  = "author"
	DeletedByManager = "manager"
)

// Comment is one entry of a ticket's conversation.
//
// A deleted comment stays as a tombstone: where it was, who wrote it and when it was removed, with no content
// and no mentions, so the conversation around it keeps its order.
type Comment struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	TicketID  string `json:"ticketId"`
	// Seq orders a ticket's comments by when they were posted: 1, 2, 3, … Editing never moves a comment.
	Seq int64 `json:"seq"`
	// AuthorID is who wrote it: a plain ID, not a foreign key, so the conversation outlives a member who leaves.
	AuthorID string `json:"authorId"`
	// AuthorName is the name they had when they wrote it, shown if they are no longer in the workspace.
	AuthorName string    `json:"authorName"`
	Segments   []Segment `json:"segments"`
	// Version moves with every edit and with the deletion; an edit is refused if it is not the one its editor read.
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"createdAt"`
	EditedAt  *time.Time `json:"editedAt,omitempty"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
	DeletedBy string     `json:"deletedBy,omitempty"`

	// ClientID and ClientDigest are the creation ID its author's client chose, and what the comment said when it was
	// created: together they make posting the same comment twice, as a client does when it retries, one comment.
	ClientID     string `json:"-"`
	ClientDigest string `json:"-"`
}

// Deleted reports whether the comment is a tombstone.
func (c Comment) Deleted() bool { return c.DeletedAt != nil }

// Notification tells one member that a comment mentions them. It stays until they mark it read, or until
// something takes it away (the mention is removed, the comment is deleted, they lose access to the project): it is
// then withdrawn, which is permanent unless a later edit mentions them again.
type Notification struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	TicketID    string     `json:"ticketId"`
	CommentID   string     `json:"commentId"`
	RecipientID string     `json:"recipientId"`
	AuthorID    string     `json:"authorId"`
	CreatedAt   time.Time  `json:"createdAt"`
	ReadAt      *time.Time `json:"readAt,omitempty"`
	Withdrawn   bool       `json:"-"`
}

// Unread reports whether the notification still waits for its recipient.
func (n Notification) Unread() bool { return n.ReadAt == nil && !n.Withdrawn }

var clientIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// CleanClientID checks the creation ID a client made up for a comment it is posting.
func CleanClientID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !clientIDRE.MatchString(s) {
		return "", invalid("a comment needs a creation id of 8 to 64 letters, digits, dashes or underscores")
	}
	return s, nil
}

// commentText cleans one piece of text: line breaks are \n, and nothing that is not text (a control character,
// or a character that reverses the direction of what follows it) gets in.
func commentText(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", invalid("a comment must be valid text")
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	for _, r := range s {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			return "", invalid("a comment contains a character that cannot be shown safely")
		}
	}
	return s, nil
}

// CleanComment turns what a client sent into what is stored. resolve says who a member ID is: their name, and
// whether they may be mentioned. The result has no empty or neighbouring text segments, no leading or trailing
// space, every mention carrying the name resolve gave, and the distinct people mentioned, in the order they first
// appear.
func CleanComment(in []Segment, resolve func(memberID string) (name string, ok bool)) ([]Segment, []string, error) {
	if len(in) == 0 {
		return nil, nil, invalid("a comment cannot be empty")
	}
	if len(in) > MaxCommentSegments {
		return nil, nil, invalid("a comment is made of at most %d parts", MaxCommentSegments)
	}
	var out []Segment
	var people []string
	seen := map[string]bool{}
	length := 0
	for _, seg := range in {
		switch seg.Type {
		case SegmentText:
			if seg.MemberID != "" {
				return nil, nil, invalid("a text part cannot name a member")
			}
			text, err := commentText(seg.Text)
			if err != nil {
				return nil, nil, err
			}
			if text == "" {
				continue
			}
			length += utf8.RuneCountInString(text)
			if n := len(out); n > 0 && out[n-1].Type == SegmentText {
				out[n-1].Text += text
			} else {
				out = append(out, Segment{Type: SegmentText, Text: text})
			}
		case SegmentMention:
			if seg.Text != "" {
				return nil, nil, invalid("a mention cannot carry text")
			}
			id := strings.TrimSpace(seg.MemberID)
			name, ok := resolve(id)
			if id == "" || !ok {
				return nil, nil, invalid("a comment can only mention people who are on this project")
			}
			if !seen[id] {
				if len(people) == MaxCommentMentions {
					return nil, nil, invalid("a comment can mention at most %d people", MaxCommentMentions)
				}
				seen[id] = true
				people = append(people, id)
			}
			length += 1 + utf8.RuneCountInString(name)
			out = append(out, Segment{Type: SegmentMention, MemberID: id, Name: name})
		default:
			return nil, nil, invalid("a comment is made of text and mentions, not %q", string(seg.Type))
		}
		if length > MaxCommentLen {
			return nil, nil, invalid("a comment is longer than %d characters", MaxCommentLen)
		}
	}
	if n := len(out); n > 0 && out[0].Type == SegmentText {
		out[0].Text = strings.TrimLeftFunc(out[0].Text, unicode.IsSpace)
		if out[0].Text == "" {
			out = out[1:]
		}
	}
	if n := len(out); n > 0 && out[n-1].Type == SegmentText {
		out[n-1].Text = strings.TrimRightFunc(out[n-1].Text, unicode.IsSpace)
		if out[n-1].Text == "" {
			out = out[:n-1]
		}
	}
	if len(out) == 0 {
		return nil, nil, invalid("a comment cannot be empty")
	}
	return out, people, nil
}

// MentionedIDs are the distinct people a comment mentions, in the order they first appear.
func MentionedIDs(segs []Segment) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range segs {
		if s.Type == SegmentMention && !seen[s.MemberID] {
			seen[s.MemberID] = true
			out = append(out, s.MemberID)
		}
	}
	return out
}

// SegmentsDigest is what a comment says, without the names it was shown with: two requests to post the same
// words and mentions have the same digest.
func SegmentsDigest(segs []Segment) string {
	type part struct {
		T SegmentKind `json:"t"`
		V string      `json:"v"`
	}
	parts := make([]part, 0, len(segs))
	for _, s := range segs {
		if s.Type == SegmentMention {
			parts = append(parts, part{s.Type, s.MemberID})
		} else {
			parts = append(parts, part{s.Type, s.Text})
		}
	}
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SameSegments reports whether two contents say the same thing (the same words, the same people).
func SameSegments(a, b []Segment) bool { return SegmentsDigest(a) == SegmentsDigest(b) }

// PlainText is a comment as one line of text, a mention as "@Name" and white space gathered, for a quotation.
func PlainText(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		if s.Type == SegmentMention {
			b.WriteString("@" + s.Name)
		} else {
			b.WriteString(s.Text)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Excerpt is the start of a comment, for a notification.
func Excerpt(segs []Segment, max int) string {
	text := PlainText(segs)
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	return strings.TrimRightFunc(string([]rune(text)[:max-1]), unicode.IsSpace) + "…"
}

// ParseSegments reads the form a comment is stored in.
func ParseSegments(raw string) ([]Segment, error) {
	var segs []Segment
	if raw == "" {
		return []Segment{}, nil
	}
	if err := json.Unmarshal([]byte(raw), &segs); err != nil {
		return nil, err
	}
	if segs == nil {
		segs = []Segment{}
	}
	return segs, nil
}

// EncodeSegments is the form a comment is stored in.
func EncodeSegments(segs []Segment) string {
	if segs == nil {
		segs = []Segment{}
	}
	raw, _ := json.Marshal(segs)
	return string(raw)
}
