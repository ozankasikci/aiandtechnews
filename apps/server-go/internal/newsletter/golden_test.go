package newsletter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// nodeGolden is testdata/node-golden.json, recorded from the real Node
// implementation by testdata/record-node.ts. Tests compare against it; they
// never infer Node's behavior.
type nodeGolden struct {
	Node     string `json:"node"`
	TimeZone string `json:"timeZone"`
	Schema   []struct {
		Type string `json:"type"`
		Name string `json:"name"`
		SQL  string `json:"sql"`
	} `json:"schema"`
	Tokens struct {
		Creates []struct {
			Secret    string  `json:"secret"`
			ID        int64   `json:"id"`
			Purpose   string  `json:"purpose"`
			ExpiresAt *string `json:"expiresAt"`
			Token     string  `json:"token"`
		} `json:"creates"`
		Verifies []struct {
			Name    string `json:"name"`
			Token   string `json:"token"`
			Purpose string `json:"purpose"`
			Secret  string `json:"secret"`
			Now     string `json:"now"`
			Result  *int64 `json:"result"`
		} `json:"verifies"`
	} `json:"tokens"`
	ReadingMinutes []struct {
		Input   *string `json:"input"`
		Minutes int     `json:"minutes"`
	} `json:"readingMinutes"`
	Emails struct {
		Welcome []struct {
			To             string    `json:"to"`
			SiteURL        string    `json:"siteUrl"`
			UnsubscribeURL string    `json:"unsubscribeUrl"`
			Email          nodeEmail `json:"email"`
		} `json:"welcome"`
		Digests []struct {
			Name           string          `json:"name"`
			To             string          `json:"to"`
			SiteURL        string          `json:"siteUrl"`
			UnsubscribeURL string          `json:"unsubscribeUrl"`
			Articles       []goldenArticle `json:"articles"`
			Email          nodeEmail       `json:"email"`
		} `json:"digests"`
		EscapeHTML         []stringPair `json:"escapeHtml"`
		EncodeURIComponent []stringPair `json:"encodeURIComponent"`
	} `json:"emails"`
	Sender     []senderCase `json:"sender"`
	Primitives struct {
		DatesParsed []struct {
			Input string `json:"input"`
			MS    *int64 `json:"ms"`
		} `json:"datesParsed"`
		IstanbulDates []struct {
			Instant string `json:"instant"`
			Edition string `json:"edition"`
		} `json:"istanbulDates"`
		ParseInts []struct {
			Input string   `json:"input"`
			Value *float64 `json:"value"`
		} `json:"parseInts"`
		Truncations []struct {
			Input   string `json:"input"`
			Slice80 string `json:"slice80"`
		} `json:"truncations"`
	} `json:"primitives"`
	DigestSelection struct {
		Now      string             `json:"now"`
		Articles []json.RawMessage  `json:"articles"`
		Result   goldenDigestResult `json:"result"`
		Editions []editionRow       `json:"editions"`
		Delivery []deliveryRow      `json:"deliveries"`
		Sent     []sentEmail        `json:"sent"`
		Delays   []int              `json:"delays"`
	} `json:"digestSelection"`
	DigestWindow struct {
		Result goldenDigestResult `json:"result"`
	} `json:"digestWindow"`
	DigestTopFive struct {
		Stamps   []string           `json:"stamps"`
		Result   goldenDigestResult `json:"result"`
		Editions []editionRow       `json:"editions"`
		Sent     []sentEmail        `json:"sent"`
	} `json:"digestTopFive"`
	DeliveryLifecycle struct {
		Subscribers  [][]json.RawMessage `json:"subscribers"`
		First        goldenDigestResult  `json:"first"`
		AfterFirst   []deliveryRow       `json:"afterFirst"`
		FirstDelays  []int               `json:"firstDelays"`
		Second       goldenDigestResult  `json:"second"`
		AfterSecond  []deliveryRow       `json:"afterSecond"`
		SecondDelays []int               `json:"secondDelays"`
		Sent         []sentEmail         `json:"sent"`
		Editions     []editionRow        `json:"editions"`
	} `json:"deliveryLifecycle"`
	Subscriptions struct {
		Outcomes []struct {
			Address   string `json:"address"`
			Placement string `json:"placement"`
			At        string `json:"at"`
			Result    *struct {
				State string `json:"state"`
			} `json:"result"`
			Error *struct {
				Name    string `json:"name"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"outcomes"`
		Subscribers []subscriberRow `json:"subscribers"`
		Normalized  []struct {
			Input string  `json:"input"`
			Email *string `json:"email"`
			Error *string `json:"error"`
		} `json:"normalized"`
	} `json:"subscriptions"`
	ConfirmAndUnsubscribe struct {
		Now      string `json:"now"`
		Confirms []struct {
			Name   string             `json:"name"`
			Token  string             `json:"token"`
			Result goldenConfirmation `json:"result"`
		} `json:"confirms"`
		Unsubscribes []struct {
			Name   string `json:"name"`
			Token  string `json:"token"`
			Result string `json:"result"`
		} `json:"unsubscribes"`
		Sent []struct {
			nodeEmail
			IdempotencyKey string `json:"idempotencyKey"`
		} `json:"sent"`
		Subscribers []subscriberRow `json:"subscribers"`
	} `json:"confirmAndUnsubscribe"`
	Editions struct {
		Lists []struct {
			Limit    int               `json:"limit"`
			Editions []json.RawMessage `json:"editions"`
		} `json:"lists"`
		Gets []struct {
			Key     string          `json:"key"`
			Edition json.RawMessage `json:"edition"`
		} `json:"gets"`
		JSONText string `json:"jsonText"`
	} `json:"editions"`
	SiteURLs []struct {
		Input   *string `json:"input"`
		SiteURL *string `json:"siteUrl"`
		Error   *struct {
			Name          string `json:"name"`
			Message       string `json:"message"`
			Configuration bool   `json:"configuration"`
		} `json:"error"`
	} `json:"siteUrls"`
	TokenSecrets struct {
		Results []struct {
			Secret      *string `json:"secret"`
			Unsubscribe *string `json:"unsubscribe"`
			Error       *struct {
				Message       string `json:"message"`
				Configuration bool   `json:"configuration"`
			} `json:"error"`
		} `json:"results"`
		TrimmedKeyResult string `json:"trimmedKeyResult"`
	} `json:"tokenSecrets"`
}

type goldenArticle struct {
	Title          string `json:"title"`
	Slug           string `json:"slug"`
	Excerpt        string `json:"excerpt"`
	Category       string `json:"category"`
	ReadingMinutes int    `json:"readingMinutes"`
}

type goldenTag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type goldenDigestResult struct {
	Edition  string `json:"edition"`
	Articles int    `json:"articles"`
	Sent     int    `json:"sent"`
	Skipped  int    `json:"skipped"`
	Failed   int    `json:"failed"`
}

type goldenConfirmation struct {
	State       string `json:"state"`
	WelcomeSent bool   `json:"welcomeSent"`
}

type stringPair struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

type nodeEmail struct {
	To      string            `json:"to"`
	Subject string            `json:"subject"`
	HTML    string            `json:"html"`
	Text    string            `json:"text"`
	Headers map[string]string `json:"headers"`
	Tags    []goldenTag       `json:"tags"`
}

type senderCase struct {
	Name     string            `json:"name"`
	Env      map[string]string `json:"env"`
	Scripted []struct {
		Status     int     `json:"status"`
		Body       *string `json:"body"`
		RetryAfter *string `json:"retryAfter"`
		Throws     *string `json:"throws"`
	} `json:"scripted"`
	Email          nodeEmail `json:"email"`
	IdempotencyKey string    `json:"idempotencyKey"`
	Calls          []struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	} `json:"calls"`
	Delays []float64 `json:"delays"`
	Result *struct {
		ID string `json:"id"`
	} `json:"result"`
	Error *struct {
		Name          string `json:"name"`
		Configuration bool   `json:"configuration"`
		Message       string `json:"message"`
	} `json:"error"`
}

type editionRow struct {
	ID         int64  `json:"id"`
	EditionKey string `json:"edition_key"`
	Subject    string `json:"subject"`
	Articles   string `json:"articles"`
	CreatedAt  string `json:"created_at"`
}

type deliveryRow struct {
	SubscriberID      int64   `json:"subscriber_id"`
	EditionKey        string  `json:"edition_key"`
	Status            string  `json:"status"`
	ProviderMessageID *string `json:"provider_message_id"`
	Error             *string `json:"error"`
	CreatedAt         string  `json:"created_at"`
	SentAt            *string `json:"sent_at"`
}

type sentEmail struct {
	To             string            `json:"to"`
	Subject        string            `json:"subject"`
	IdempotencyKey string            `json:"idempotencyKey"`
	HTML           string            `json:"html"`
	Text           string            `json:"text"`
	Headers        map[string]string `json:"headers"`
	Tags           []goldenTag       `json:"tags"`
}

type subscriberRow struct {
	ID                 int64   `json:"id"`
	Email              string  `json:"email"`
	Status             string  `json:"status"`
	SourcePlacement    *string `json:"source_placement"`
	ConfirmationSentAt *string `json:"confirmation_sent_at"`
	ConfirmedAt        *string `json:"confirmed_at"`
	UnsubscribedAt     *string `json:"unsubscribed_at"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

var (
	goldenOnce  sync.Once
	goldenValue nodeGolden
	goldenErr   error
)

func golden(t testing.TB) *nodeGolden {
	t.Helper()
	goldenOnce.Do(func() {
		data, err := os.ReadFile(filepath.Join("testdata", "node-golden.json"))
		if err != nil {
			goldenErr = err
			return
		}
		goldenErr = json.Unmarshal(data, &goldenValue)
	})
	if goldenErr != nil {
		t.Fatalf("load node-golden.json: %v", goldenErr)
	}
	return &goldenValue
}
