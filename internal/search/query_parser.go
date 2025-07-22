package search

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"snail-cli/internal/interfaces"
)

// QueryParser parses search query strings into SearchQuery structures
type QueryParser struct {
	// Regular expressions for parsing different query components
	fromRegex    *regexp.Regexp
	toRegex      *regexp.Regexp
	subjectRegex *regexp.Regexp
	labelRegex   *regexp.Regexp
	statusRegex  *regexp.Regexp
	dateRegex    *regexp.Regexp
	limitRegex   *regexp.Regexp
	offsetRegex  *regexp.Regexp
	sortRegex    *regexp.Regexp
}

// NewQueryParser creates a new query parser
func NewQueryParser() *QueryParser {
	return &QueryParser{
		fromRegex:    regexp.MustCompile(`from:([^\s]+)`),
		toRegex:      regexp.MustCompile(`to:([^\s]+)`),
		subjectRegex: regexp.MustCompile(`subject:([^\s]+)`),
		labelRegex:   regexp.MustCompile(`label:([^\s]+)`),
		statusRegex:  regexp.MustCompile(`status:(unread|read|draft|sent)`),
		dateRegex:    regexp.MustCompile(`date:([^\s]+)`),
		limitRegex:   regexp.MustCompile(`limit:(\d+)`),
		offsetRegex:  regexp.MustCompile(`offset:(\d+)`),
		sortRegex:    regexp.MustCompile(`sort:(relevance|date|from|subject)(?::(asc|desc))?`),
	}
}

// ParseQuery parses a query string into a SearchQuery structure
func (qp *QueryParser) ParseQuery(queryStr string) (*interfaces.SearchQuery, error) {
	query := &interfaces.SearchQuery{
		SortBy:    interfaces.SortByRelevance,
		SortOrder: interfaces.SortDesc,
	}

	// Remove extra whitespace
	queryStr = strings.TrimSpace(queryStr)
	if queryStr == "" {
		return query, nil
	}

	// Parse sort first (to avoid conflicts with date regex)
	if matches := qp.sortRegex.FindStringSubmatch(queryStr); len(matches) > 1 {
		sortBy, sortOrder, err := qp.parseSortFromMatches(matches)
		if err != nil {
			return nil, fmt.Errorf("invalid sort '%s': %w", matches[0], err)
		}
		query.SortBy = sortBy
		query.SortOrder = sortOrder
		queryStr = qp.sortRegex.ReplaceAllString(queryStr, "")
	}

	// Parse limit
	if matches := qp.limitRegex.FindStringSubmatch(queryStr); len(matches) > 1 {
		limit, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("invalid limit '%s': %w", matches[1], err)
		}
		query.Limit = limit
		queryStr = qp.limitRegex.ReplaceAllString(queryStr, "")
	}

	// Parse offset
	if matches := qp.offsetRegex.FindStringSubmatch(queryStr); len(matches) > 1 {
		offset, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("invalid offset '%s': %w", matches[1], err)
		}
		query.Offset = offset
		queryStr = qp.offsetRegex.ReplaceAllString(queryStr, "")
	}

	// Parse status
	if matches := qp.statusRegex.FindStringSubmatch(queryStr); len(matches) > 1 {
		status, err := qp.parseStatus(matches[1])
		if err != nil {
			return nil, fmt.Errorf("invalid status '%s': %w", matches[1], err)
		}
		query.Status = status
		queryStr = qp.statusRegex.ReplaceAllString(queryStr, "")
	}

	// Parse from field
	if matches := qp.fromRegex.FindStringSubmatch(queryStr); len(matches) > 1 {
		query.From = matches[1]
		queryStr = qp.fromRegex.ReplaceAllString(queryStr, "")
	}

	// Parse to field
	if matches := qp.toRegex.FindStringSubmatch(queryStr); len(matches) > 1 {
		query.To = matches[1]
		queryStr = qp.toRegex.ReplaceAllString(queryStr, "")
	}

	// Parse subject field
	if matches := qp.subjectRegex.FindStringSubmatch(queryStr); len(matches) > 1 {
		query.Subject = matches[1]
		queryStr = qp.subjectRegex.ReplaceAllString(queryStr, "")
	}

	// Parse labels
	labelMatches := qp.labelRegex.FindAllStringSubmatch(queryStr, -1)
	for _, match := range labelMatches {
		if len(match) > 1 {
			query.Labels = append(query.Labels, match[1])
		}
	}
	queryStr = qp.labelRegex.ReplaceAllString(queryStr, "")

	// Parse date range (after sort to avoid conflicts)
	if matches := qp.dateRegex.FindStringSubmatch(queryStr); len(matches) > 1 {
		dateFrom, dateTo, err := qp.parseDateRange(matches[1])
		if err != nil {
			return nil, fmt.Errorf("invalid date range '%s': %w", matches[1], err)
		}
		query.DateFrom = dateFrom
		query.DateTo = dateTo
		queryStr = qp.dateRegex.ReplaceAllString(queryStr, "")
	}

	// Remaining text is the full-text search query
	query.Text = strings.TrimSpace(queryStr)

	return query, nil
}

// parseStatus parses a status string into EmailStatus
func (qp *QueryParser) parseStatus(statusStr string) (interfaces.EmailStatus, error) {
	switch strings.ToLower(statusStr) {
	case "unread":
		return interfaces.StatusUnread, nil
	case "read":
		return interfaces.StatusRead, nil
	case "draft":
		return interfaces.StatusDraft, nil
	case "sent":
		return interfaces.StatusSent, nil
	default:
		return "", fmt.Errorf("unknown status: %s", statusStr)
	}
}

// parseDateRange parses a date range string into from and to dates
func (qp *QueryParser) parseDateRange(dateStr string) (*time.Time, *time.Time, error) {
	// Support various date formats:
	// - "2024-01-15" (single date, matches that day)
	// - "2024-01-15..2024-01-20" (date range)
	// - "1d" (last 1 day)
	// - "1w" (last 1 week)
	// - "1m" (last 1 month)
	// - "1y" (last 1 year)

	if strings.Contains(dateStr, "..") {
		// Date range
		parts := strings.Split(dateStr, "..")
		if len(parts) != 2 {
			return nil, nil, fmt.Errorf("invalid date range format")
		}

		from, err := qp.parseDate(parts[0])
		if err != nil {
			return nil, nil, fmt.Errorf("invalid from date: %w", err)
		}

		to, err := qp.parseDate(parts[1])
		if err != nil {
			return nil, nil, fmt.Errorf("invalid to date: %w", err)
		}

		return from, to, nil
	}

	// Check for relative dates (1d, 1w, 1m, 1y)
	if len(dateStr) >= 2 {
		unit := dateStr[len(dateStr)-1:]
		if unit == "d" || unit == "w" || unit == "m" || unit == "y" {
			numStr := dateStr[:len(dateStr)-1]
			num, err := strconv.Atoi(numStr)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid relative date number: %w", err)
			}

			now := time.Now()
			var from time.Time

			switch unit {
			case "d":
				from = now.AddDate(0, 0, -num)
			case "w":
				from = now.AddDate(0, 0, -num*7)
			case "m":
				from = now.AddDate(0, -num, 0)
			case "y":
				from = now.AddDate(-num, 0, 0)
			}

			return &from, &now, nil
		}
	}

	// Single date
	date, err := qp.parseDate(dateStr)
	if err != nil {
		return nil, nil, err
	}

	// For single date, match the entire day
	from := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	to := from.AddDate(0, 0, 1).Add(-time.Nanosecond)

	return &from, &to, nil
}

// parseDate parses a date string into a time.Time
func (qp *QueryParser) parseDate(dateStr string) (*time.Time, error) {
	// Support various date formats
	formats := []string{
		"2006-01-02",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
		"01/02/2006",
		"01-02-2006",
	}

	for _, format := range formats {
		if date, err := time.Parse(format, dateStr); err == nil {
			return &date, nil
		}
	}

	return nil, fmt.Errorf("unsupported date format: %s", dateStr)
}

// parseSortFromMatches parses sort field and order from regex matches
func (qp *QueryParser) parseSortFromMatches(matches []string) (interfaces.SortField, interfaces.SortOrder, error) {
	if len(matches) < 2 {
		return "", "", fmt.Errorf("invalid sort matches")
	}

	fieldStr := matches[1]
	orderStr := "desc" // default

	if len(matches) > 2 && matches[2] != "" {
		orderStr = matches[2]
	}

	// Parse field
	var field interfaces.SortField
	switch strings.ToLower(fieldStr) {
	case "relevance":
		field = interfaces.SortByRelevance
	case "date":
		field = interfaces.SortByDate
	case "from":
		field = interfaces.SortByFrom
	case "subject":
		field = interfaces.SortBySubject
	default:
		return "", "", fmt.Errorf("unknown sort field: %s", fieldStr)
	}

	// Parse order
	var order interfaces.SortOrder
	switch strings.ToLower(orderStr) {
	case "asc":
		order = interfaces.SortAsc
	case "desc":
		order = interfaces.SortDesc
	default:
		return "", "", fmt.Errorf("unknown sort order: %s", orderStr)
	}

	return field, order, nil
}

// parseSort parses a sort string into SortField and SortOrder
func (qp *QueryParser) parseSort(sortStr string) (interfaces.SortField, interfaces.SortOrder, error) {
	// Support formats:
	// - "date" (default desc)
	// - "date:asc"
	// - "date:desc"
	// - "relevance"
	// - "from"
	// - "subject"

	parts := strings.Split(sortStr, ":")
	fieldStr := parts[0]
	orderStr := "desc" // default

	if len(parts) > 1 {
		orderStr = parts[1]
	}

	// Parse field
	var field interfaces.SortField
	switch strings.ToLower(fieldStr) {
	case "relevance":
		field = interfaces.SortByRelevance
	case "date":
		field = interfaces.SortByDate
	case "from":
		field = interfaces.SortByFrom
	case "subject":
		field = interfaces.SortBySubject
	default:
		return "", "", fmt.Errorf("unknown sort field: %s", fieldStr)
	}

	// Parse order
	var order interfaces.SortOrder
	switch strings.ToLower(orderStr) {
	case "asc":
		order = interfaces.SortAsc
	case "desc":
		order = interfaces.SortDesc
	default:
		return "", "", fmt.Errorf("unknown sort order: %s", orderStr)
	}

	return field, order, nil
}

// BuildQueryString builds a query string from a SearchQuery structure
func (qp *QueryParser) BuildQueryString(query *interfaces.SearchQuery) string {
	var parts []string

	// Add text query first
	if query.Text != "" {
		parts = append(parts, query.Text)
	}

	// Add metadata filters
	if query.From != "" {
		parts = append(parts, fmt.Sprintf("from:%s", query.From))
	}

	if query.To != "" {
		parts = append(parts, fmt.Sprintf("to:%s", query.To))
	}

	if query.Subject != "" {
		parts = append(parts, fmt.Sprintf("subject:%s", query.Subject))
	}

	for _, label := range query.Labels {
		parts = append(parts, fmt.Sprintf("label:%s", label))
	}

	if query.Status != "" {
		parts = append(parts, fmt.Sprintf("status:%s", query.Status))
	}

	// Add date range
	if query.DateFrom != nil || query.DateTo != nil {
		if query.DateFrom != nil && query.DateTo != nil {
			parts = append(parts, fmt.Sprintf("date:%s..%s",
				query.DateFrom.Format("2006-01-02"),
				query.DateTo.Format("2006-01-02")))
		} else if query.DateFrom != nil {
			parts = append(parts, fmt.Sprintf("date:%s..",
				query.DateFrom.Format("2006-01-02")))
		} else if query.DateTo != nil {
			parts = append(parts, fmt.Sprintf("date:..%s",
				query.DateTo.Format("2006-01-02")))
		}
	}

	// Add pagination
	if query.Limit > 0 {
		parts = append(parts, fmt.Sprintf("limit:%d", query.Limit))
	}

	if query.Offset > 0 {
		parts = append(parts, fmt.Sprintf("offset:%d", query.Offset))
	}

	// Add sort
	if query.SortBy != interfaces.SortByRelevance || query.SortOrder != interfaces.SortDesc {
		sortStr := string(query.SortBy)
		if query.SortOrder == interfaces.SortAsc {
			sortStr += ":asc"
		}
		parts = append(parts, fmt.Sprintf("sort:%s", sortStr))
	}

	return strings.Join(parts, " ")
}

// ValidateQuery validates a SearchQuery structure
func (qp *QueryParser) ValidateQuery(query *interfaces.SearchQuery) error {
	if query == nil {
		return fmt.Errorf("query cannot be nil")
	}

	// Validate status
	if query.Status != "" {
		switch query.Status {
		case interfaces.StatusUnread, interfaces.StatusRead, interfaces.StatusDraft, interfaces.StatusSent:
			// Valid status
		default:
			return fmt.Errorf("invalid status: %s", query.Status)
		}
	}

	// Validate sort field
	switch query.SortBy {
	case interfaces.SortByRelevance, interfaces.SortByDate, interfaces.SortByFrom, interfaces.SortBySubject:
		// Valid sort field
	default:
		return fmt.Errorf("invalid sort field: %s", query.SortBy)
	}

	// Validate sort order
	switch query.SortOrder {
	case interfaces.SortAsc, interfaces.SortDesc:
		// Valid sort order
	default:
		return fmt.Errorf("invalid sort order: %s", query.SortOrder)
	}

	// Validate date range
	if query.DateFrom != nil && query.DateTo != nil {
		if query.DateFrom.After(*query.DateTo) {
			return fmt.Errorf("date from cannot be after date to")
		}
	}

	// Validate pagination
	if query.Limit < 0 {
		return fmt.Errorf("limit cannot be negative")
	}

	if query.Offset < 0 {
		return fmt.Errorf("offset cannot be negative")
	}

	return nil
}

// GetQueryHelp returns help text for query syntax
func (qp *QueryParser) GetQueryHelp() string {
	return `Search Query Syntax:

Text Search:
  Simply type words to search in email content, subject, and sender names.
  Example: "project meeting"

Filters:
  from:sender@example.com    - Filter by sender email
  to:recipient@example.com   - Filter by recipient email
  subject:keyword            - Filter by subject content
  label:work                 - Filter by label (can use multiple)
  status:unread              - Filter by status (unread, read, draft, sent)

Date Filters:
  date:2024-01-15            - Emails from specific date
  date:2024-01-15..2024-01-20 - Date range
  date:1d                    - Last 1 day
  date:1w                    - Last 1 week
  date:1m                    - Last 1 month
  date:1y                    - Last 1 year

Pagination:
  limit:10                   - Limit results to 10 emails
  offset:20                  - Skip first 20 results

Sorting:
  sort:date                  - Sort by date (newest first)
  sort:date:asc              - Sort by date (oldest first)
  sort:relevance             - Sort by relevance (default)
  sort:from                  - Sort by sender
  sort:subject               - Sort by subject

Examples:
  "project meeting from:alice@example.com status:unread"
  "label:work date:1w sort:date:asc"
  "urgent subject:deadline date:2024-01-01..2024-01-31"
`
}