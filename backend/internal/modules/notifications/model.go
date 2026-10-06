package notifications

import "time"

const (
	ChannelInApp = "in_app"
	ChannelEmail = "email"
	ChannelSMS   = "sms"
)

type Notification struct {
	ID        string     `json:"id"`
	EventType string     `json:"event_type"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Icon      string     `json:"icon,omitempty"`
	Severity  string     `json:"severity"`
	LinkURL   string     `json:"link_url,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type Preference struct {
	ScopeKind string `json:"scope_kind"`
	ScopeID   string `json:"scope_id"`
	EventType string `json:"event_type"`
	Channel   string `json:"channel"`
	Enabled   bool   `json:"enabled"`
}

type Template struct {
	Title    string
	Body     string
	Icon     string
	Severity string
}

var templates = map[string]Template{
	"account.welcome":                     {"Karibu LipaGO", "Workspace yako iko tayari. Kamilisha uthibitishaji ili kuwezesha malipo ya moja kwa moja.", "👋", "info"},
	"payment.succeeded":                   {"Malipo yamefanikiwa", "Malipo ya {amount} {currency} yamepokelewa kwa mafanikio.", "✓", "success"},
	"payment.failed":                      {"Malipo hayakufanikiwa", "Malipo ya {amount} {currency} hayakukamilika.", "!", "alert"},
	"payment.expired":                     {"Agizo limekwisha muda", "Agizo la malipo ya {amount} {currency} limekwisha muda bila kukamilika.", "⏱", "warning"},
	"payment.refunded":                    {"Malipo yamerejeshwa", "Marejesho ya {amount} {currency} yameanzishwa.", "↩", "info"},
	"withdrawal.requested":                {"Ombi la kutoa fedha limepokelewa", "Ombi la kutoa {amount} {currency} linasubiri hatua inayofuata.", "→", "info"},
	"withdrawal.requires_approval":        {"Idhini ya kutoa fedha inahitajika", "Ombi la kutoa {amount} {currency} linasubiri idhini ya mhudumu wa pili.", "!", "warning"},
	"withdrawal.approved":                 {"Ombi la kutoa fedha limekubaliwa", "Ombi la kutoa {amount} {currency} limekubaliwa.", "✓", "success"},
	"withdrawal.rejected":                 {"Ombi la kutoa fedha limekataliwa", "Ombi la kutoa {amount} {currency} limekataliwa: {reason}", "!", "alert"},
	"withdrawal.dispatched":               {"Malipo ya kutoa fedha yametumwa", "Ombi la kutoa {amount} {currency} limetumwa kwa mtoa huduma.", "→", "info"},
	"withdrawal.completed":                {"Kutoa fedha kumekamilika", "Kutoa {amount} {currency} kumekamilika.", "✓", "success"},
	"withdrawal.failed":                   {"Kutoa fedha hakukufanikiwa", "Kutoa {amount} {currency} hakukufanikiwa: {reason}", "!", "alert"},
	"kyc.submitted":                       {"Uthibitishaji umewasilishwa", "Nyaraka zako za uthibitishaji zimepokelewa na zinapitiwa.", "▣", "info"},
	"kyc.verified":                        {"Akaunti imethibitishwa", "Uthibitishaji wa akaunti yako umekubaliwa. Sasa unaweza kutumia huduma za moja kwa moja.", "✓", "success"},
	"kyc.rejected":                        {"Uthibitishaji haujakubaliwa", "Uthibitishaji wa akaunti yako haujakubaliwa: {reason}", "!", "alert"},
	"webhook.delivery_failed":             {"Webhook inahitaji umakini", "Uwasilishaji wa webhook umeshindwa mara kadhaa.", "⚠", "warning"},
	"org.suspended":                       {"Akaunti imesimamishwa", "Akaunti yako imesimamishwa: {reason}", "!", "alert"},
	"org.unsuspended":                     {"Akaunti imerejeshwa", "Akaunti yako sasa iko hai tena.", "✓", "success"},
	"org.limits_changed":                  {"Vikomo vya akaunti vimebadilika", "Vikomo vya miamala ya akaunti yako vimesasishwa.", "↕", "warning"},
	"org.fee_override_changed":            {"Ada za akaunti zimesasishwa", "Mipangilio ya ada za akaunti yako imebadilika.", "%", "info"},
	"org.member_invited":                  {"Mwaliko wa timu", "Umealikwa kujiunga na workspace ya LipaGO.", "+", "info"},
	"org.role_changed":                    {"Jukumu la timu limebadilika", "Jukumu lako katika workspace limesasishwa kuwa {role}.", "◆", "info"},
	"creator.contribution_received":       {"Mchango mpya umepokelewa", "Ukurasa wako wa malipo umepokea mchango wa {amount} {currency}.", "♥", "success"},
	"security.payout_destination_changed": {"Njia ya malipo imebadilika", "Mpokeaji wa malipo yako amesasishwa. Wasiliana na timu ikiwa hukuanzisha mabadiliko haya.", "⚠", "alert"},
	"security.api_key_rotated":            {"API key imezungushwa", "API key mpya imeundwa na ya zamani iko kwenye kipindi cha neema.", "⌘", "warning"},
	"security.api_key_grace_ending":       {"API key inakaribia kuisha", "Kipindi cha neema cha API key yako kinaisha hivi karibuni.", "⌘", "warning"},
	"admin.kyc_queue":                     {"KYC mpya inasubiri mapitio", "Akaunti mpya imewasilisha KYC na inahitaji mapitio.", "▣", "info"},
	"admin.webhook_alert":                 {"Tahadhari ya webhook", "Endpoint ya webhook imevuka kikomo cha majaribio na inahitaji ukaguzi.", "⚠", "warning"},
	"admin.reconciliation_alert":          {"Tahadhari ya upatanisho", "Tatizo la upatanisho wa malipo linahitaji ukaguzi.", "⚠", "warning"},
	"admin.payment_failed":                {"Malipo hayakufanikiwa", "{body}", "!", "alert"},
	"admin.broadcast":                     {"Ujumbe wa LipaGO", "{body}", "", "info"},
	"support.ticket_created":              {"Tiketi mpya ya msaada", "Tiketi mpya imewasilishwa kwa timu ya LipaGO: {subject}.", "?", "info"},
	"support.customer_replied":            {"Jibu jipya la tiketi", "Mteja ameongeza jibu kwenye tiketi: {subject}.", "↩", "info"},
	"support.admin_replied":               {"Timu ya LipaGO imejibu", "Kuna jibu jipya kwenye tiketi yako: {subject}.", "↩", "info"},
	"support.status_changed":              {"Hali ya tiketi imebadilika", "Tiketi yako {subject} sasa iko katika hali ya {status}.", "•", "info"},
	"support.ticket_resolved":             {"Tiketi imetatuliwa", "Tiketi yako {subject} imetiwa alama kuwa imetatuliwa.", "✓", "success"},
}
