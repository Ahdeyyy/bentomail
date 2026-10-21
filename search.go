package main

func (a *app) searchMail() {
	query, revision, s := a.query, a.state.Revision, a.store
	if query == "" {
		a.searchResult = nil
		a.searchResultQuery = ""
		return
	}
	var result map[string]bool
	a.run("Search complete", func() error { var err error; result, err = s.SearchIDs(query); return err }, func(err error) {
		if err == nil && a.query == query && a.state.Revision == revision {
			a.searchResult = result
			a.searchResultQuery = query
			a.searchRevision = revision
		}
	})
}
