package request

// Pagination defaults. A request that omits a parameter gets these rather than
// a rejection, so `GET /resource` is a valid call.
const (
	DefaultPage  = 1
	DefaultLimit = 20
	// MaxLimit is the ceiling the binding tag enforces. It is repeated here so
	// callers can quote it, and the tag below is what actually rejects.
	MaxLimit = 100
	// MaxPage bounds the page number. Without a ceiling, Offset multiplies two
	// large ints and overflows to a negative number; GORM only emits OFFSET when
	// it is positive, so the clause is silently dropped and the caller gets
	// page 1's rows labelled as page 92233720368547760. An empty page is the
	// correct answer, so the ceiling is what makes it reachable.
	MaxPage = 1000000
)

// Pagination is the shared query-string window for list endpoints.
//
// It binds from the query string, not the body: these are `?page=2&limit=50`
// parameters on a GET, so it is read with c.ShouldBindQuery and carries `form:`
// tags. A `json:` tag here would bind nothing.
//
// Every rule is `omitempty` because the zero value has to mean "unset". Without
// it, `min=1` would reject an absent `page` — a client that simply asked for
// the first page of a collection would get a validation error.
//
// That makes an explicit `?page=0` indistinguishable from an absent one, so it
// yields page 1 rather than a rejection. Go has no way to tell "0" from "not
// sent" on an int, and the alternative — pointer fields, or a parallel set of
// "was it present" booleans — costs more complexity at every call site than the
// case is worth. A negative page is still rejected, which catches the mistake
// that actually happens: arithmetic that ran off the end of a list.
//
// The ceiling on Limit is the load-bearing rule. Without it a client asks for
// limit=1000000 and the database does the work: a full table scan, a result set
// that has to be materialised in memory and serialised, and a response that
// ties up a connection for as long as it takes. That is a denial of service
// that costs the caller one query string. A refused request is cheap; an
// unbounded one is not.
// The json tags do no binding work — nothing decodes a Pagination from a body
// — but the validator's registered tag-name function reads the json tag to
// decide what to call a field in an error. Without them a rejected `?limit=500`
// comes back under the key "Limit", which is not what the client sent and not
// what it can highlight.
type Pagination struct {
	Page  int `form:"page"  json:"page"  binding:"omitempty,min=1,max=1000000"`
	Limit int `form:"limit" json:"limit" binding:"omitempty,min=1,max=100"`
}

// Normalized returns a copy with the defaults applied.
//
// It returns a copy rather than mutating the receiver so that the bound request
// still shows what the client actually asked for, which is what an access log
// or an error message should report.
func (p Pagination) Normalized() Pagination {
	if p.Page < DefaultPage {
		p.Page = DefaultPage
	}
	if p.Page > MaxPage {
		p.Page = MaxPage
	}
	if p.Limit < 1 {
		p.Limit = DefaultLimit
	}
	if p.Limit > MaxLimit {
		p.Limit = MaxLimit
	}
	return p
}

// Offset is the number of rows to skip for this page.
//
// It normalises first, so calling it on an unbound zero value yields 0 rather
// than the negative offset a raw `(0-1)*0` would give.
func (p Pagination) Offset() int {
	n := p.Normalized()
	return (n.Page - 1) * n.Limit
}
