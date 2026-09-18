package main

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
)

// Handbook entries come in two kinds. The API spells the second one
// "pipeline"; the app, the handbook tree, and this command surface all call it
// a playbook, so that is the word the CLI uses and `pipelines` stays as the
// low-level generated group.
const (
	kindPage     = "page"
	kindPlaybook = "playbook"
)

// errHandbookNotFound is what handbookAPI.get returns for a 404, so callers can
// turn it into a reference error that names what was missing.
var errHandbookNotFound = errors.New("not found")

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// handbookAPI issues the account-scoped requests the handbook commands are
// built from. Every method in this file is a GET: discovery never writes
// handbook content and never spawns a task. The writes the authoring and
// organization commands make live in handbookedit.go, and none of them creates
// a task either.
type handbookAPI struct {
	accountID string

	// index caches the handbook tree. Name and path references resolve
	// against it, and it is the only place a caller can learn an entry's
	// path or its children, so it is worth fetching once per invocation.
	index *handbookIndex
}

func newHandbookAPI(accountID string) (*handbookAPI, error) {
	if accountID == "" {
		return nil, errors.New("account_id not set in config. Run: wallfacer auth login")
	}
	return &handbookAPI{accountID: accountID}, nil
}

func (a *handbookAPI) get(path string, query url.Values) (map[string]interface{}, error) {
	server := viper.GetString("server")
	if server == "" {
		server = openapiServers()[viper.GetInt("server-index")]["url"]
	}

	req := cli.Client.Get().URL(server + path)
	for key, values := range query {
		for _, value := range values {
			req = req.AddQuery(key, value)
		}
	}

	resp, err := req.Do()
	if err != nil {
		return nil, errors.Wrap(err, "Request failed")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, errHandbookNotFound
	}
	if resp.StatusCode >= 400 {
		return nil, errors.Errorf("HTTP %d: %s", resp.StatusCode, resp.String())
	}

	var decoded map[string]interface{}
	if err := cli.UnmarshalResponse(resp, &decoded); err != nil {
		return nil, errors.Wrap(err, "Unmarshalling response failed")
	}
	return decoded, nil
}

func (a *handbookAPI) accountPath(format string, args ...interface{}) string {
	return "/v1/accounts/" + url.PathEscape(a.accountID) + fmt.Sprintf(format, args...)
}

func (a *handbookAPI) getTree() ([]interface{}, error) {
	resp, err := a.get(a.accountPath("/handbook"), nil)
	if err != nil {
		return nil, err
	}
	data, err := responseObject(resp)
	if err != nil {
		return nil, err
	}
	tree, _ := data["tree"].([]interface{})
	return tree, nil
}

func (a *handbookAPI) getPage(id string) (map[string]interface{}, error) {
	resp, err := a.get(a.accountPath("/pages/%s", url.PathEscape(id)), nil)
	if err != nil {
		return nil, err
	}
	return responseObject(resp)
}

func (a *handbookAPI) getPipeline(id string) (map[string]interface{}, error) {
	resp, err := a.get(a.accountPath("/pipelines/%s", url.PathEscape(id)), nil)
	if err != nil {
		return nil, err
	}
	return responseObject(resp)
}

func (a *handbookAPI) getPipelineVersion(id, version string) (map[string]interface{}, error) {
	resp, err := a.get(a.accountPath("/pipelines/%s/versions/%s", url.PathEscape(id), url.PathEscape(version)), nil)
	if err != nil {
		return nil, err
	}
	return responseObject(resp)
}

func (a *handbookAPI) listPipelineVersions(id string) (map[string]interface{}, error) {
	return a.get(a.accountPath("/pipelines/%s/versions", url.PathEscape(id)), nil)
}

func (a *handbookAPI) listPageRevisions(id string, query url.Values) (map[string]interface{}, error) {
	return a.get(a.accountPath("/pages/%s/revisions", url.PathEscape(id)), query)
}

func (a *handbookAPI) getPageRevision(pageID, revisionID string) (map[string]interface{}, error) {
	resp, err := a.get(a.accountPath("/pages/%s/revisions/%s", url.PathEscape(pageID), url.PathEscape(revisionID)), nil)
	if err != nil {
		return nil, err
	}
	return responseObject(resp)
}

func (a *handbookAPI) listPages(query url.Values) (map[string]interface{}, error) {
	return a.get(a.accountPath("/pages"), query)
}

// findDeletedPage looks a deleted page up in the list endpoint, which is the
// only page read that includes trashed rows: `GET /pages/{page}` binds the
// active row only and 404s on a deleted ID. The sweep is unbounded because an
// ID either belongs to the account or does not, and a page that happens to sort
// past the search default would otherwise be reported as missing.
func (a *handbookAPI) findDeletedPage(id string) (map[string]interface{}, error) {
	var found map[string]interface{}
	_, err := a.sweep(func(query url.Values) (map[string]interface{}, error) {
		query.Set("include_deleted", "true")
		return a.listPages(query)
	}, sweepUnbounded, func(record map[string]interface{}) bool {
		if stringField(record, "id") != id {
			return true
		}
		found = record
		return false
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, errHandbookNotFound
	}
	return found, nil
}

// readPage reads the record behind a resolved page reference. A deleted page is
// readable only through the include-deleted list, so `handbook read <id>`
// reaches the same rows the resolver does instead of 404ing on the show route.
func (a *handbookAPI) readPage(ref *handbookRef) (map[string]interface{}, error) {
	if ref.State == "deleted" {
		return a.findDeletedPage(ref.ID)
	}
	record, err := a.getPage(ref.ID)
	if err == errHandbookNotFound {
		// Deleted between the resolve and the read, or resolved off a tree
		// node that has since been trashed.
		if deleted, deletedErr := a.findDeletedPage(ref.ID); deletedErr == nil {
			return deleted, nil
		}
	}
	return record, err
}

func (a *handbookAPI) listPipelines(query url.Values) (map[string]interface{}, error) {
	return a.get(a.accountPath("/pipelines"), query)
}

// handbookRef is one resolved handbook entry: the stable ID a later write or
// run has to use, the resource type, the account it belongs to, and the state
// a caller must account for before acting on it. Authoring, publication, and
// execution commands resolve through here, so every surface accepts the same
// reference forms and reports the same identity.
type handbookRef struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	AccountID    string `json:"account_id"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	Path         string `json:"path,omitempty"`
	ParentPageID string `json:"parent_page_id,omitempty"`
	State        string `json:"state"`
	ResolvedFrom string `json:"resolved_from"`

	HasBody       *bool                  `json:"has_body,omitempty"`
	HasDraft      *bool                  `json:"has_draft,omitempty"`
	ActiveVersion map[string]interface{} `json:"active_version,omitempty"`
	VersionCount  *float64               `json:"version_count,omitempty"`
	LinkedPageIDs []interface{}          `json:"linked_page_ids,omitempty"`
	MatchedIn     []string               `json:"matched_in,omitempty"`

	// versionHint carries the version a playbook-version URL named. It is
	// not part of the entry's identity, so it stays out of the output.
	versionHint string
}

// handbookIndex is the handbook tree flattened into one lookup table. The tree
// holds active entries only, which is exactly the set names and paths resolve
// against.
type handbookIndex struct {
	raw        []interface{}
	entries    []*handbookRef
	byID       map[string]*handbookRef
	byTitle    map[string][]*handbookRef
	byPath     map[string][]*handbookRef
	childrenOf map[string][]*handbookRef
}

func (a *handbookAPI) loadIndex() (*handbookIndex, error) {
	if a.index != nil {
		return a.index, nil
	}

	tree, err := a.getTree()
	if err != nil {
		return nil, err
	}

	idx := &handbookIndex{
		raw:        tree,
		byID:       map[string]*handbookRef{},
		byTitle:    map[string][]*handbookRef{},
		byPath:     map[string][]*handbookRef{},
		childrenOf: map[string][]*handbookRef{},
	}
	idx.walk(tree, "", "", a.accountID)
	a.index = idx
	return idx, nil
}

func (idx *handbookIndex) walk(nodes []interface{}, parentID, prefix, accountID string) {
	for _, item := range nodes {
		node, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		ref := refFromTreeNode(node, accountID, parentID, prefix)
		if ref == nil {
			continue
		}

		idx.entries = append(idx.entries, ref)
		idx.byID[ref.ID] = ref
		idx.byTitle[strings.ToLower(ref.Title)] = append(idx.byTitle[strings.ToLower(ref.Title)], ref)
		idx.byPath[normalizeHandbookPath(ref.Path)] = append(idx.byPath[normalizeHandbookPath(ref.Path)], ref)
		if parentID != "" {
			idx.childrenOf[parentID] = append(idx.childrenOf[parentID], ref)
		}

		if children, ok := node["children"].([]interface{}); ok && len(children) > 0 {
			idx.walk(children, ref.ID, ref.Path, accountID)
		}
	}
}

func refFromTreeNode(node map[string]interface{}, accountID, parentID, prefix string) *handbookRef {
	id := stringField(node, "id")
	if id == "" {
		return nil
	}

	title := stringField(node, "title")
	path := title
	if prefix != "" {
		path = prefix + "/" + title
	}

	ref := &handbookRef{
		Type:         stringField(node, "type"),
		ID:           id,
		AccountID:    accountID,
		Title:        title,
		Description:  stringField(node, "description"),
		Path:         path,
		ParentPageID: parentID,
		State:        "active",
		ResolvedFrom: "tree",
	}

	switch ref.Type {
	case kindPage:
		if hasBody, ok := node["has_body"].(bool); ok {
			ref.HasBody = &hasBody
		}
	case kindPlaybook:
		if hasDraft, ok := node["has_draft"].(bool); ok {
			ref.HasDraft = &hasDraft
		}
		if active, ok := node["active_version"].(map[string]interface{}); ok {
			ref.ActiveVersion = active
		}
		if stringField(node, "disabled_at") != "" {
			ref.State = "disabled"
		}
	}

	return ref
}

// refFromPageRecord builds a reference from a page record, which unlike a tree
// node can be deleted.
func refFromPageRecord(record map[string]interface{}, accountID, resolvedFrom string) *handbookRef {
	ref := &handbookRef{
		Type:         kindPage,
		ID:           stringField(record, "id"),
		AccountID:    accountID,
		Title:        stringField(record, "title"),
		Description:  stringField(record, "description"),
		ParentPageID: stringField(record, "parent_page_id"),
		State:        "active",
		ResolvedFrom: resolvedFrom,
	}
	hasBody := stringField(record, "body") != ""
	ref.HasBody = &hasBody
	if stringField(record, "deleted_at") != "" {
		ref.State = "deleted"
	}
	return ref
}

// refFromPipelineRecord builds a reference from a pipeline record, which unlike
// a tree node can be archived and carries the linked pages and version counts a
// caller needs for the next read.
func refFromPipelineRecord(record map[string]interface{}, accountID, resolvedFrom string) *handbookRef {
	ref := &handbookRef{
		Type:         kindPlaybook,
		ID:           stringField(record, "id"),
		AccountID:    accountID,
		Title:        stringField(record, "name"),
		Description:  stringField(record, "description"),
		ParentPageID: stringField(record, "parent_page_id"),
		State:        "active",
		ResolvedFrom: resolvedFrom,
	}

	hasDraft := record["draft"] != nil
	ref.HasDraft = &hasDraft
	if active, ok := record["active_version"].(map[string]interface{}); ok {
		ref.ActiveVersion = map[string]interface{}{"id": active["id"], "version": active["version"]}
	}
	if count, ok := record["version_count"].(float64); ok {
		ref.VersionCount = &count
	}
	if linked, ok := record["linked_page_ids"].([]interface{}); ok {
		ref.LinkedPageIDs = linked
	}

	switch {
	case stringField(record, "archived_at") != "":
		ref.State = "archived"
	case stringField(record, "disabled_at") != "":
		ref.State = "disabled"
	}
	return ref
}

// ambiguousRefError reports every candidate a name or path matched, so the
// caller can pick one instead of the CLI guessing.
type ambiguousRefError struct {
	reference  string
	candidates []*handbookRef
}

func (e *ambiguousRefError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d handbook entries; use an ID or a full path:", e.reference, len(e.candidates))
	for _, c := range e.candidates {
		fmt.Fprintf(&b, "\n  %-8s  %s  %s", c.Type, c.ID, c.Path)
	}
	return b.String()
}

// resolveHandbookRef turns any supported reference into exactly one typed
// target. want is kindPage, kindPlaybook, or "" to accept either.
//
// Accepted forms: a stable ID, a Wallfacer page or playbook detail URL inside
// the configured account, a `wallfacer://handbook/pages/<id>` link, a full path
// through the tree, and a unique entry name. Names and paths resolve against
// active entries only; IDs and URLs also reach deleted pages and archived
// playbooks, which is what a restore needs.
func (a *handbookAPI) resolveHandbookRef(reference, want string) (*handbookRef, error) {
	trimmed := strings.TrimSpace(reference)
	if trimmed == "" {
		return nil, errors.New("a handbook reference is required")
	}

	locator, err := parseHandbookLocator(trimmed)
	if err != nil {
		return nil, err
	}

	if locator == nil {
		return a.resolveByName(trimmed, want)
	}

	// Account mismatch is rejected before any read, so a reference from
	// another account can never reach a mutation or a run.
	if locator.accountID != "" && !strings.EqualFold(locator.accountID, a.accountID) {
		return nil, errors.Errorf("reference %q belongs to account %s, but the configured account is %s", reference, locator.accountID, a.accountID)
	}
	if locator.kind != "" && want != "" && locator.kind != want {
		return nil, errors.Errorf("reference %q names a %s, but a %s was requested", reference, locator.kind, want)
	}

	kind := locator.kind
	if kind == "" {
		kind = want
	}

	ref, err := a.resolveByID(reference, locator.id, kind)
	if err != nil {
		return nil, err
	}
	ref.versionHint = locator.versionID
	return ref, nil
}

func (a *handbookAPI) resolveByID(reference, id, kind string) (*handbookRef, error) {
	idx, err := a.loadIndex()
	if err != nil {
		return nil, err
	}

	if ref, ok := idx.byID[id]; ok {
		if kind != "" && ref.Type != kind {
			return nil, errors.Errorf("%q is a %s, but a %s was requested", reference, ref.Type, kind)
		}
		clone := *ref
		clone.ResolvedFrom = "id"
		return &clone, nil
	}

	// Not in the tree: either a deleted page, an archived playbook, or an ID
	// this account does not hold.
	if kind == "" || kind == kindPage {
		record, err := a.getPage(id)
		if err == nil {
			return refFromPageRecord(record, a.accountID, "id"), nil
		}
		if err != errHandbookNotFound {
			return nil, err
		}
		record, err = a.findDeletedPage(id)
		if err == nil {
			return refFromPageRecord(record, a.accountID, "id"), nil
		}
		if err != errHandbookNotFound {
			return nil, err
		}
	}
	if kind == "" || kind == kindPlaybook {
		record, err := a.getPipeline(id)
		if err == nil {
			return refFromPipelineRecord(record, a.accountID, "id"), nil
		}
		if err != errHandbookNotFound {
			return nil, err
		}
	}

	// A typed request that missed may still have named the other type; say so
	// rather than reporting it as absent.
	if kind == kindPage {
		if _, err := a.getPipeline(id); err == nil {
			return nil, errors.Errorf("%q is a playbook, but a page was requested", reference)
		}
	}
	if kind == kindPlaybook {
		if _, err := a.getPage(id); err == nil {
			return nil, errors.Errorf("%q is a page, but a playbook was requested", reference)
		}
	}

	return nil, errors.Errorf("no %s found for %q in account %s", describeKind(kind), reference, a.accountID)
}

func (a *handbookAPI) resolveByName(reference, want string) (*handbookRef, error) {
	idx, err := a.loadIndex()
	if err != nil {
		return nil, err
	}

	var matches []*handbookRef
	if strings.Contains(reference, "/") {
		matches = idx.byPath[normalizeHandbookPath(reference)]
	} else {
		matches = idx.byTitle[strings.ToLower(reference)]
	}

	if want != "" {
		var typed []*handbookRef
		for _, m := range matches {
			if m.Type == want {
				typed = append(typed, m)
			}
		}
		if len(typed) == 0 && len(matches) > 0 {
			return nil, errors.Errorf("%q is a %s, but a %s was requested", reference, matches[0].Type, want)
		}
		matches = typed
	}

	switch len(matches) {
	case 0:
		return nil, errors.Errorf("no active %s named %q in account %s", describeKind(want), reference, a.accountID)
	case 1:
		clone := *matches[0]
		clone.ResolvedFrom = "path"
		if !strings.Contains(reference, "/") {
			clone.ResolvedFrom = "name"
		}
		return &clone, nil
	default:
		return nil, &ambiguousRefError{reference: reference, candidates: matches}
	}
}

// handbookLocator is a reference that already carries a stable ID: a bare ID,
// a detail URL, or a wallfacer:// handbook link.
type handbookLocator struct {
	id        string
	kind      string
	accountID string
	versionID string
}

func parseHandbookLocator(reference string) (*handbookLocator, error) {
	if uuidPattern.MatchString(reference) {
		return &handbookLocator{id: reference}, nil
	}

	lower := strings.ToLower(reference)
	switch {
	case strings.HasPrefix(lower, "wallfacer://"):
		parsed, err := url.Parse(reference)
		if err != nil {
			return nil, errors.Errorf("%q is not a readable wallfacer:// reference", reference)
		}
		segments := pathSegments(parsed.Host + "/" + parsed.Path)
		if len(segments) == 3 && segments[0] == "handbook" && segments[1] == "pages" && uuidPattern.MatchString(segments[2]) {
			return &handbookLocator{id: segments[2], kind: kindPage}, nil
		}
		return nil, errors.Errorf("%q is not a wallfacer:// handbook page reference", reference)

	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		parsed, err := url.Parse(reference)
		if err != nil {
			return nil, errors.Errorf("%q is not a readable URL", reference)
		}
		locator := locatorFromURLPath(pathSegments(parsed.Path))
		if locator == nil {
			return nil, errors.Errorf("%q is not a Wallfacer handbook page or playbook URL", reference)
		}
		return locator, nil
	}

	return nil, nil
}

// locatorFromURLPath recognises the handbook detail routes the app serves
// today: the account-scoped ones and the browse ones.
func locatorFromURLPath(segments []string) *handbookLocator {
	var accountID string
	var rest []string

	switch {
	case len(segments) >= 3 && segments[0] == "accounts" && segments[2] == "handbook":
		accountID, rest = segments[1], segments[3:]
	case len(segments) >= 2 && segments[0] == "handbook":
		accountID, rest = segments[1], segments[2:]
	default:
		return nil
	}

	if !uuidPattern.MatchString(accountID) {
		return nil
	}

	switch {
	case len(rest) == 2 && rest[0] == "pages" && uuidPattern.MatchString(rest[1]):
		return &handbookLocator{id: rest[1], kind: kindPage, accountID: accountID}
	case len(rest) == 1 && uuidPattern.MatchString(rest[0]):
		return &handbookLocator{id: rest[0], kind: kindPlaybook, accountID: accountID}
	case len(rest) == 3 && rest[1] == "versions" && uuidPattern.MatchString(rest[0]):
		return &handbookLocator{id: rest[0], kind: kindPlaybook, accountID: accountID, versionID: rest[2]}
	}
	return nil
}

func pathSegments(path string) []string {
	var out []string
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			out = append(out, segment)
		}
	}
	return out
}

func normalizeHandbookPath(path string) string {
	segments := pathSegments(path)
	for i, segment := range segments {
		segments[i] = strings.ToLower(strings.TrimSpace(segment))
	}
	return strings.Join(segments, "/")
}

func describeKind(kind string) string {
	switch kind {
	case kindPage:
		return "page"
	case kindPlaybook:
		return "playbook"
	default:
		return "page or playbook"
	}
}

func responseObject(resp map[string]interface{}) (map[string]interface{}, error) {
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil, errors.New("unexpected response format: missing data object")
	}
	return data, nil
}

func responseList(resp map[string]interface{}) []interface{} {
	items, _ := resp["data"].([]interface{})
	return items
}

func stringField(record map[string]interface{}, key string) string {
	value, _ := record[key].(string)
	return value
}

// versionArgument renders a version number the way the versions endpoint wants
// it in the path. The API takes either a UUID or an integer version number.
func versionArgument(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	default:
		return ""
	}
}
