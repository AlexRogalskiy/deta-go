package deta

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	// ErrTooManyItems too many items
	ErrTooManyItems = errors.New("too many items")
	// ErrBadDestination bad destination
	ErrBadDestination = errors.New("bad destination")
	// ErrBadItem = errors.New("bad items")
	ErrBadItem = errors.New("bad item")
)

// Base deta base
type Base struct {
	// deta api client
	client *detaClient

	// auth info for authenticating requests
	auth *authInfo

	// Util base utilities
	Util *util
}

// Items always stored as a map of string to interface{}
type baseItem map[string]interface{}

// Query datatype
type Query []map[string]interface{}

// Updates datatype
type Updates map[string]interface{}

// NewBase returns a pointer to a new Base
func newBase(projectKey, baseName, rootEndpoint string) *Base {
	parts := strings.Split(projectKey, "_")
	projectID := parts[0]

	// root endpoint for the base
	rootEndpoint = fmt.Sprintf("%s/%s/%s", rootEndpoint, projectID, baseName)

	return &Base{
		client: newDetaClient(rootEndpoint, &authInfo{
			authType:    "api-key",
			headerKey:   "X-API-Key",
			headerValue: projectKey,
		}),
	}
}

func (b *Base) removeEmptyKey(bi baseItem) error {
	key, ok := bi["key"]
	if !ok {
		return nil
	}
	switch key.(type) {
	case string:
		if key == "" {
			delete(bi, "key")
		}
		return nil
	default:
		return fmt.Errorf("%w: %v", ErrBadItem, "Key is not a string")
	}
}

func (b *Base) modifyItem(item interface{}) (baseItem, error) {
	data, err := json.Marshal(item)
	if err != nil {
		return nil, ErrBadItem
	}
	var bi baseItem
	err = json.Unmarshal(data, &bi)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadItem, err)
	}
	err = b.removeEmptyKey(bi)
	if err != nil {
		return nil, err
	}
	return bi, nil
}

// modifies items to a []baseItem
func (b *Base) modifyItems(items interface{}) ([]baseItem, error) {
	data, err := json.Marshal(items)
	if err != nil {
		return nil, ErrBadItem
	}
	var bi []baseItem
	err = json.Unmarshal(data, &bi)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadItem, err)
	}
	for _, item := range bi {
		err = b.removeEmptyKey(item)
		if err != nil {
			return nil, err
		}
	}
	return bi, nil
}

type putResponse struct {
	Processed map[string][]baseItem `json:"processed"`
	Failed    map[string][]baseItem `json:"failed"`
}

func (b *Base) put(items []baseItem) ([]string, error) {
	body := map[string]interface{}{
		"items": items,
	}
	o, err := b.client.request(&requestInput{
		Path:   "/items",
		Method: "PUT",
		Body:   body,
	})
	if err != nil {
		return nil, err
	}

	var pr putResponse
	err = json.Unmarshal(o.Body, &pr)
	if err != nil {
		return nil, err
	}

	var keys []string
	for _, item := range pr.Processed["items"] {
		keys = append(keys, item["key"].(string))
	}

	return keys, nil
}

// Put operation for Deta Base
// Put puts a new item in the database under the provided key
// If item with the same key already exists in the database, the existing item is overwritten
// If the 'key' is empty a key is autogenerated
func (b *Base) Put(item interface{}) (string, error) {
	if item == nil {
		return "", nil
	}

	items := []interface{}{item}
	modifiedItems, err := b.modifyItems(items)
	if err != nil {
		return "", err
	}

	putKeys, err := b.put(modifiedItems)
	if err != nil {
		return "", err
	}
	return putKeys[0], nil
}

// PutMany operation for Deta Base
// Puts at most 25 items at a time
func (b *Base) PutMany(items interface{}) ([]string, error) {
	modifiedItems, err := b.modifyItems(items)
	if err != nil {
		return nil, err
	}

	if len(modifiedItems) == 0 {
		return nil, nil
	}
	if len(modifiedItems) > 25 {
		return nil, ErrTooManyItems
	}
	return b.put(modifiedItems)
}

// Get gets an item with 'key' from the database
// the item is scanned onto `dest`
func (b *Base) Get(key string, dest interface{}) error {
	escapedKey := url.PathEscape(key)
	o, err := b.client.request(&requestInput{
		Path:   fmt.Sprintf("/items/%s", escapedKey),
		Method: "GET",
	})
	if err != nil {
		return err
	}
	err = json.Unmarshal(o.Body, &dest)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadDestination, err)
	}
	return nil
}

type insertRequest struct {
	Item baseItem `json:"item"`
}

// Insert inserts an item in the database only if the key does not exist
func (b *Base) Insert(item interface{}) (string, error) {
	modifiedItem, err := b.modifyItem(item)
	if err != nil {
		return "", err
	}

	ir := &insertRequest{
		Item: modifiedItem,
	}

	o, err := b.client.request(&requestInput{
		Path:   "/items",
		Method: "POST",
		Body:   ir,
	})

	if err != nil {
		return "", err
	}

	var bi baseItem
	err = json.Unmarshal(o.Body, &bi)
	if err != nil {
		return "", err
	}
	return bi["key"].(string), nil
}

type updateRequest struct {
	Set       map[string]interface{} `json:"set"`
	Trim      []string               `json:"trim"`
	Append    map[string]interface{} `json:"append"`
	Prepend   map[string]interface{} `json:"prepend"`
	Increment map[string]interface{} `json:"increment"`
}

// converts updates to an update request
func (b *Base) updatesToUpdateRequest(updates Updates) *updateRequest {
	updateReq := &updateRequest{
		Set:       make(map[string]interface{}),
		Append:    make(map[string]interface{}),
		Prepend:   make(map[string]interface{}),
		Increment: make(map[string]interface{}),
	}
	for k, v := range updates {
		switch v.(type) {
		case *trimUtil:
			updateReq.Trim = append(updateReq.Trim, k)
		case *appendUtil:
			updateReq.Append[k] = v.(*appendUtil).value
		case *prependUtil:
			updateReq.Prepend[k] = v.(*prependUtil).value
		case *incrementUtil:
			updateReq.Increment[k] = v.(*incrementUtil).value
		default:
			updateReq.Set[k] = v
		}
	}
	return updateReq
}

// Update updates the item with the 'key' with the provide 'updates'
func (b *Base) Update(key string, updates Updates) error {
	// escape key
	escapedKey := url.PathEscape(key)

	ur := b.updatesToUpdateRequest(updates)
	_, err := b.client.request(&requestInput{
		Path:   fmt.Sprintf("/items/%s", escapedKey),
		Method: "PATCH",
		Body:   ur,
	})
	if err != nil {
		return err
	}
	return nil
}

// Delete deletes an item from the database
func (b *Base) Delete(key string) error {
	// escape the key
	escapedKey := url.PathEscape(key)

	_, err := b.client.request(&requestInput{
		Path:   fmt.Sprintf("/items/%s", escapedKey),
		Method: "DELETE",
	})
	if err != nil {
		return err
	}
	return nil
}

type paging struct {
	Size int     `json:"size"`
	Last *string `json:"last"`
}

type fetchRequest struct {
	Query Query   `json:"query"`
	Last  *string `json:"last,omitempty"`
	Limit *int    `json:"limit,omitempty"`
}

type fetchResponse struct {
	Paging *paging       `json:"paging"`
	Items  []interface{} `json:"items"`
}

func (b *Base) fetch(req *fetchRequest) (*fetchResponse, error) {
	o, err := b.client.request(&requestInput{
		Path:   fmt.Sprintf("/query"),
		Method: "POST",
		Body:   req,
	})
	if err != nil {
		return nil, err
	}
	var fr fetchResponse
	err = json.Unmarshal(o.Body, &fr)
	if err != nil {
		return nil, err
	}
	return &fr, nil
}

// Fetch fetches maximum 'limit' items from the database based on the 'query'
// Provide a 'limit' value of 0 or less to apply no limits
// It scans the result onto 'dest'
// A nil query fetches all items from the database
// Fetch is paginated, returns the last key fetched if further pages are left
func (b *Base) Fetch(query Query, dest interface{}, limit int) (string, error) {
	req := &fetchRequest{
		Query: query,
	}
	if limit > 0 {
		req.Limit = &limit
	}

	res, err := b.fetch(req)
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(res.Items)
	if err != nil {
		return "", err
	}
	err = json.Unmarshal(data, &dest)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBadDestination, err)
	}

	lastKey := ""
	if res.Paging.Last != nil {
		lastKey = *res.Paging.Last
	}
	return lastKey, nil
}
