package internal

type Wiki struct {
	Domain string
	Handle string
}

var (
	wikiAE  = Wiki{Domain: "alter-ego.fandom.com", Handle: "alter-ego"}
	wikiTDS = Wiki{Domain: "tds.wiki", Handle: "tds.wiki"}
)

const (
	prefixAE  = "aew_"
	prefixTDS = "tdsw_"
)
