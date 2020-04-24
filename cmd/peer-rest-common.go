package cmd

const (
	peerRESTVersion       = "v8"
	peerRESTVersionPrefix = SlashSeparator + peerRESTVersion
	peerRESTPrefix        = radioReservedBucketPath + "/peer"
	peerRESTPath          = peerRESTPrefix + peerRESTVersionPrefix
)

const (
	peerRESTMethodDeleteJournalRec = "/deletejournalrec"
	peerRESTMethodPutJournalRec    = "/putjournalrec"
)

const (
	peerRESTJournalDir = "journaldir"
)
