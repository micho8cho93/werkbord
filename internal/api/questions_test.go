package api

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"devboard/internal/agent"
	"devboard/internal/domain"
)

type questionReply struct {
	apiError
	Question *domain.Question `json:"question"`
}

func (rs *runsServer) pendingQuestions(t *testing.T) []domain.Question {
	t.Helper()
	var qs struct{ Questions []domain.Question }
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/questions", "", &qs); code != 200 {
		t.Fatalf("list questions: %d", code)
	}
	return qs.Questions
}

func (rs *runsServer) waitQuestions(t *testing.T, n int) []domain.Question {
	t.Helper()
	waitFor(t, "pending questions", func() bool { return len(rs.pendingQuestions(t)) == n })
	return rs.pendingQuestions(t)
}

func TestQuestionOverHTTP(t *testing.T) {
	rs := newRunsServer(t)
	task := rs.task(t, "Choose a store")
	run := rs.start(t, task)
	s := rs.adapter.Last()
	s.AskQuestion(agent.Question{
		Ref: "r", Kind: domain.QuestionDecision, Prompt: "Postgres or SQLite?", Context: "Postgres needs a server.",
		Options: []string{"Postgres", "SQLite"}, AllowFreeText: false,
	})
	q := rs.waitQuestions(t, 1)[0]

	// What the phone needs to show it, without asking anything else.
	if q.RunID != run.ID || q.TaskID != task.ID || q.ProjectID != rs.project.ID || q.Kind != domain.QuestionDecision ||
		q.Context != "Postgres needs a server." || q.AllowFreeText || len(q.Options) != 2 || q.State != domain.QuestionPending || q.AskedAt.IsZero() {
		t.Fatalf("question = %+v", q)
	}
	var one domain.Question
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+q.ID, "", &one); code != 200 || one.ID != q.ID || one.Prompt != q.Prompt {
		t.Fatalf("get = %d %+v", code, one)
	}
	if code := do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/questions/qst_nope", "", nil); code != 404 {
		t.Fatalf("unknown question: %d", code)
	}

	// The task stays where it is: there is no Needs Input column.
	var tasks struct{ Tasks []domain.Task }
	do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/tasks", "", &tasks)
	if tasks.Tasks[0].State != domain.TaskDoing {
		t.Fatalf("task = %+v", tasks.Tasks[0])
	}

	// An answer that is not one of the choices is refused and changes nothing.
	var bad apiError
	for _, body := range []string{`{"answer":"MySQL"}`, `{"answer":"  "}`, `{}`} {
		if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+q.ID+"/answer", body, &bad); code != 400 || bad.Error.Code != "invalid" {
			t.Fatalf("answer %s: %d %+v", body, code, bad)
		}
	}
	if got := rs.pendingQuestions(t); len(got) != 1 || len(s.Responses()) != 0 {
		t.Fatalf("a refused answer changed something: %+v", got)
	}

	// The answer, in another case; the reply is the question as it now stands.
	var answered domain.Question
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+q.ID+"/answer", `{"answer":"sqlite"}`, &answered); code != 200 ||
		answered.State != domain.QuestionAnswered || answered.Answer != "SQLite" || answered.DeliveredAt == nil || answered.AnsweredAt == nil {
		t.Fatalf("answer = %d %+v", code, answered)
	}
	if r := s.Responses(); len(r) != 1 || r[0].Answer != "SQLite" {
		t.Fatalf("agent got %+v", r)
	}
	if rs.getRun(t, run.ID).State != domain.RunRunning {
		t.Fatal("the run should be working again")
	}

	// A retry after a lost reply succeeds with the same question.
	var retry domain.Question
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+q.ID+"/answer", `{"answer":"SQLite"}`, &retry); code != 200 || retry.ID != q.ID || retry.Answer != "SQLite" {
		t.Fatalf("retry = %d %+v", code, retry)
	}
	if len(s.Responses()) != 1 {
		t.Fatal("a retry must not reach the agent again")
	}

	// A competing answer from another device loses, and is shown the winner.
	var lost questionReply
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+q.ID+"/answer", `{"answer":"Postgres"}`, &lost); code != 409 ||
		lost.Error.Code != "question_answered" || lost.Question == nil || lost.Question.Answer != "SQLite" || lost.Question.State != domain.QuestionAnswered {
		t.Fatalf("competing answer = %d %+v", code, lost)
	}
}

func TestAnsweringAClosedQuestionOverHTTP(t *testing.T) {
	rs := newRunsServer(t)
	run := rs.start(t, rs.task(t, "Will exit"))
	s := rs.adapter.Last()
	s.Ask("r", "Proceed?")
	q := rs.waitQuestions(t, 1)[0]

	s.Exit(1, "crashed")
	waitFor(t, "the run to fail", func() bool { return rs.getRun(t, run.ID).State == domain.RunFailed })

	var reply questionReply
	code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+q.ID+"/answer", `{"answer":"yes"}`, &reply)
	if code != 409 || reply.Error.Code != "question_closed" || !strings.Contains(reply.Error.Message, "agent stopped") ||
		reply.Question == nil || reply.Question.State != domain.QuestionCancelled || reply.Question.CancelReason != domain.CancelRunEnded {
		t.Fatalf("answer = %d %+v", code, reply)
	}
	if len(rs.pendingQuestions(t)) != 0 {
		t.Fatal("a cancelled question is not pending")
	}
}

// A client that was away, or a second browser that opens later, rebuilds what
// it missed from the event stream's resume point: the question, and its end.
func TestQuestionEventsReplayToAReconnectingClient(t *testing.T) {
	rs := newRunsServer(t)
	run := rs.start(t, rs.task(t, "Replayed"))
	s := rs.adapter.Last()

	var page struct{ Events []domain.Event }
	do(t, "GET", rs.url+"/api/projects/"+rs.project.ID+"/runs/"+run.ID+"/events", "", &page)
	last := page.Events[len(page.Events)-1].Seq // all this client saw

	s.Ask("a", "First?")
	s.Ask("b", "Second?")
	qs := rs.waitQuestions(t, 2)
	if code := do(t, "POST", rs.url+"/api/projects/"+rs.project.ID+"/questions/"+qs[0].ID+"/answer", `{"answer":"yes"}`, nil); code != 200 {
		t.Fatalf("answer: %d", code)
	}
	s.WithdrawQuestion("b")
	waitFor(t, "the second question to close", func() bool { return len(rs.pendingQuestions(t)) == 0 })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", rs.url+"/api/events", nil)
	req.Header.Set("Last-Event-ID", itoa(last)) // what EventSource sends on reconnect
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var types []string
	r := bufio.NewReader(resp.Body)
	want := map[string]int{"agent.question": 2, "question.answered": 1, "question.cancelled": 1}
	got := map[string]int{}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended: %v (saw %v)", err, types)
		}
		if typ, ok := strings.CutPrefix(strings.TrimSpace(line), "event: "); ok {
			types = append(types, typ)
			got[typ]++
		}
		done := true
		for k, n := range want {
			if got[k] < n {
				done = false
			}
		}
		if done {
			break
		}
	}
	// In the order they happened: both asked, then the answer, then the withdrawal.
	var order []string
	for _, ty := range types {
		if _, ok := want[ty]; ok {
			order = append(order, ty)
		}
	}
	if strings.Join(order, ",") != "agent.question,agent.question,question.answered,question.cancelled" {
		t.Fatalf("replayed question events = %v", order)
	}
}
