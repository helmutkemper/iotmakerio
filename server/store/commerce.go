// server/store/commerce.go — DB access for the BOM and store redirect system.
//
// Covers:
//   - components           — physical chip/module catalogue
//   - blackbox_components  — maps Go struct names to components
//   - stores               — external stores per country
//   - store_listings       — component ↔ store join with redirect info
//   - store_redirect_log   — click analytics
package store

import (
	"database/sql"
	"time"
)

// ─── Types ────────────────────────────────────────────────────────────────────

// Component is one physical chip or module in the catalogue.
type Component struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	DatasheetURL string    `json:"datasheet_url"`
	ImageURL     string    `json:"image_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// BlackBoxComponent maps a Go struct name to a physical component.
type BlackBoxComponent struct {
	ID           string    `json:"id"`
	BlackBoxName string    `json:"blackbox_name"`
	ComponentID  string    `json:"component_id"`
	Quantity     int       `json:"quantity"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"created_at"`
}

// Store is one external store serving a country.
type Store struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	CountryCode  string    `json:"country_code"`
	BaseURL      string    `json:"base_url"`
	AffiliateTag string    `json:"affiliate_tag"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StoreListing is one component available at one store.
type StoreListing struct {
	ID          string    `json:"id"`
	ComponentID string    `json:"component_id"`
	StoreID     string    `json:"store_id"`
	ProductURL  string    `json:"product_url"`
	PriceHint   string    `json:"price_hint"`
	Currency    string    `json:"currency"`
	InStock     bool      `json:"in_stock"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Resolved — populated by the handler for the BOM API response.
	StoreName    string `json:"store_name,omitempty"`
	StoreBaseURL string `json:"store_base_url,omitempty"`
}

// BOMEntry is one line in the Bill of Materials returned to the WASM.
// It groups a component with all its store listings for the user's country.
type BOMEntry struct {
	Component *Component      `json:"component"`
	Quantity  int             `json:"quantity"` // from blackbox_components
	Listings  []*StoreListing `json:"listings"`
}

// RedirectLog is one store redirect click event.
type RedirectLog struct {
	ID        string    `json:"id"`
	ListingID string    `json:"listing_id"`
	UserID    *string   `json:"user_id"`
	IPCountry string    `json:"ip_country"`
	ClickedAt time.Time `json:"clicked_at"`
}

// ─── BOM query ────────────────────────────────────────────────────────────────

// GetBOMForBlackBoxes returns one BOMEntry per unique component needed by the
// given list of black-box struct names, with store listings for the given country.
//
// blackBoxNames may contain duplicates (same chip placed multiple times on the
// canvas). The returned quantity per component is the sum of all placements.
//
// Only active stores are included. If no listings exist for a component in the
// requested country, the component appears in the BOM with an empty Listings slice
// so the maker still knows what they need even if no local store carries it.
func GetBOMForBlackBoxes(blackBoxNames []string, countryCode string) ([]*BOMEntry, error) {
	if len(blackBoxNames) == 0 {
		return []*BOMEntry{}, nil
	}

	// Count how many times each struct appears (one per canvas placement).
	nameCounts := map[string]int{}
	for _, n := range blackBoxNames {
		nameCounts[n]++
	}

	// Build IN (...) clause.
	placeholders := make([]string, len(nameCounts))
	args := make([]any, 0, len(nameCounts))
	i := 0
	for name := range nameCounts {
		placeholders[i] = "?"
		args = append(args, name)
		i++
	}
	inClause := joinStrings(placeholders, ",")

	// Fetch all (component, blackbox_name, quantity) rows for the requested names.
	rows, err := DB.Query(`
		SELECT
			bc.blackbox_name, bc.quantity,
			c.id, c.name, c.description, c.datasheet_url, c.image_url,
			c.created_at, c.updated_at
		FROM blackbox_components bc
		JOIN components c ON c.id = bc.component_id
		WHERE bc.blackbox_name IN (`+inClause+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// componentID → BOMEntry (aggregate quantities across placements).
	entryMap := map[string]*BOMEntry{}
	for rows.Next() {
		var bbName string
		var bbQty int
		comp := &Component{}
		var createdStr, updatedStr string
		if err := rows.Scan(
			&bbName, &bbQty,
			&comp.ID, &comp.Name, &comp.Description, &comp.DatasheetURL, &comp.ImageURL,
			&createdStr, &updatedStr,
		); err != nil {
			return nil, err
		}
		comp.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		comp.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)

		// placements × quantity_per_instance.
		totalQty := nameCounts[bbName] * bbQty

		if entry, ok := entryMap[comp.ID]; ok {
			entry.Quantity += totalQty
		} else {
			entryMap[comp.ID] = &BOMEntry{
				Component: comp,
				Quantity:  totalQty,
				Listings:  []*StoreListing{},
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch listings for each component in the user's country.
	for compID, entry := range entryMap {
		listings, err := getListingsForComponent(compID, countryCode)
		if err != nil {
			return nil, err
		}
		entry.Listings = listings
	}

	// Convert map to slice, ordered by component name.
	bom := make([]*BOMEntry, 0, len(entryMap))
	for _, e := range entryMap {
		bom = append(bom, e)
	}
	sortBOMByName(bom)
	return bom, nil
}

// getListingsForComponent returns all active store listings for a component
// in the given country, including store name and base URL for link building.
func getListingsForComponent(componentID, countryCode string) ([]*StoreListing, error) {
	rows, err := DB.Query(`
		SELECT
			sl.id, sl.component_id, sl.store_id, sl.product_url,
			sl.price_hint, sl.currency, sl.in_stock, sl.updated_at,
			s.name, s.base_url
		FROM store_listings sl
		JOIN stores s ON s.id = sl.store_id
		WHERE sl.component_id = ?
		  AND s.country_code  = ?
		  AND s.active        = 1
		ORDER BY s.name ASC`,
		componentID, countryCode,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	listings := []*StoreListing{}
	for rows.Next() {
		l := &StoreListing{}
		var inStockInt int
		var updatedStr string
		if err := rows.Scan(
			&l.ID, &l.ComponentID, &l.StoreID, &l.ProductURL,
			&l.PriceHint, &l.Currency, &inStockInt, &updatedStr,
			&l.StoreName, &l.StoreBaseURL,
		); err != nil {
			return nil, err
		}
		l.InStock = inStockInt == 1
		l.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		listings = append(listings, l)
	}
	return listings, rows.Err()
}

// GetListingWithStore returns one store listing and its parent store.
// Used by the redirect handler to build the final URL.
func GetListingWithStore(listingID string) (*StoreListing, *Store, error) {
	l := &StoreListing{}
	s := &Store{}
	var inStockInt, storeActiveInt int
	var listingUpdated, storeCreated, storeUpdated string

	err := DB.QueryRow(`
		SELECT
			sl.id, sl.component_id, sl.store_id, sl.product_url,
			sl.price_hint, sl.currency, sl.in_stock, sl.updated_at,
			s.id, s.name, s.country_code, s.base_url, s.affiliate_tag,
			s.active, s.created_at, s.updated_at
		FROM store_listings sl
		JOIN stores s ON s.id = sl.store_id
		WHERE sl.id = ?`, listingID,
	).Scan(
		&l.ID, &l.ComponentID, &l.StoreID, &l.ProductURL,
		&l.PriceHint, &l.Currency, &inStockInt, &listingUpdated,
		&s.ID, &s.Name, &s.CountryCode, &s.BaseURL, &s.AffiliateTag,
		&storeActiveInt, &storeCreated, &storeUpdated,
	)
	if err == sql.ErrNoRows {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	l.InStock = inStockInt == 1
	s.Active = storeActiveInt == 1
	l.UpdatedAt, _ = time.Parse(time.RFC3339, listingUpdated)
	s.CreatedAt, _ = time.Parse(time.RFC3339, storeCreated)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, storeUpdated)
	return l, s, nil
}

// LogRedirect records one store redirect click.
// id must be generated by the handler using cryptoauth.MustNewID().
func LogRedirect(id, listingID string, userID *string, ipCountry string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO store_redirect_log (id, listing_id, user_id, ip_country, clicked_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, listingID, userID, ipCountry, now,
	)
	return err
}

// ─── Components — admin CRUD ──────────────────────────────────────────────────

// ListComponents returns all components ordered by name.
func ListComponents() ([]*Component, error) {
	rows, err := DB.Query(`
		SELECT id, name, description, datasheet_url, image_url, created_at, updated_at
		FROM components ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanComponents(rows)
}

// GetComponent returns one component by ID.
func GetComponent(id string) (*Component, error) {
	rows, err := DB.Query(`
		SELECT id, name, description, datasheet_url, image_url, created_at, updated_at
		FROM components WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cs, err := scanComponents(rows)
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, ErrNotFound
	}
	return cs[0], nil
}

// CreateComponent inserts a new component.
func CreateComponent(c *Component) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO components (id, name, description, datasheet_url, image_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Description, c.DatasheetURL, c.ImageURL, now, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// UpdateComponent replaces all mutable fields.
func UpdateComponent(c *Component) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := DB.Exec(`
		UPDATE components SET name=?, description=?, datasheet_url=?, image_url=?, updated_at=?
		WHERE id=?`,
		c.Name, c.Description, c.DatasheetURL, c.ImageURL, now, c.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteComponent removes a component. Fails if active store listings reference it.
func DeleteComponent(id string) error {
	var count int
	_ = DB.QueryRow(`SELECT COUNT(*) FROM store_listings WHERE component_id=?`, id).Scan(&count)
	if count > 0 {
		return ErrConflict // use a specific error in a real implementation
	}
	res, err := DB.Exec(`DELETE FROM components WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListBlackBoxComponents returns all black-box associations for a component.
func ListBlackBoxComponents(componentID string) ([]*BlackBoxComponent, error) {
	rows, err := DB.Query(`
		SELECT id, blackbox_name, component_id, quantity, notes, created_at
		FROM blackbox_components WHERE component_id=? ORDER BY blackbox_name ASC`, componentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBlackBoxComponents(rows)
}

// AddBlackBoxComponent creates a new black-box → component association.
func AddBlackBoxComponent(b *BlackBoxComponent) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO blackbox_components (id, blackbox_name, component_id, quantity, notes, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		b.ID, b.BlackBoxName, b.ComponentID, b.Quantity, b.Notes, now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// DeleteBlackBoxComponent removes one black-box association.
func DeleteBlackBoxComponent(id string) error {
	res, err := DB.Exec(`DELETE FROM blackbox_components WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ─── Stores — admin CRUD ──────────────────────────────────────────────────────

// ListStores returns all stores ordered by country then name.
func ListStores() ([]*Store, error) {
	rows, err := DB.Query(`
		SELECT id, name, country_code, base_url, affiliate_tag, active, created_at, updated_at
		FROM stores ORDER BY country_code ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanStores(rows)
}

// CreateStore inserts a new store.
func CreateStore(s *Store) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO stores (id, name, country_code, base_url, affiliate_tag, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.CountryCode, s.BaseURL, s.AffiliateTag, boolToInt(s.Active), now, now,
	)
	return err
}

// UpdateStore replaces all mutable fields of a store.
func UpdateStore(s *Store) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := DB.Exec(`
		UPDATE stores SET name=?, country_code=?, base_url=?, affiliate_tag=?, active=?, updated_at=?
		WHERE id=?`,
		s.Name, s.CountryCode, s.BaseURL, s.AffiliateTag, boolToInt(s.Active), now, s.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListStoreListings returns all listings for one store.
func ListStoreListings(storeID string) ([]*StoreListing, error) {
	rows, err := DB.Query(`
		SELECT sl.id, sl.component_id, sl.store_id, sl.product_url,
		       sl.price_hint, sl.currency, sl.in_stock, sl.updated_at,
		       s.name, s.base_url
		FROM store_listings sl
		JOIN stores s ON s.id = sl.store_id
		WHERE sl.store_id=? ORDER BY sl.updated_at DESC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanListings(rows)
}

// AddStoreListing creates a new listing.
func AddStoreListing(l *StoreListing) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := DB.Exec(`
		INSERT INTO store_listings (id, component_id, store_id, product_url, price_hint, currency, in_stock, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		l.ID, l.ComponentID, l.StoreID, l.ProductURL, l.PriceHint, l.Currency, boolToInt(l.InStock), now,
	)
	if err != nil && isSQLiteConstraint(err) {
		return ErrConflict
	}
	return err
}

// UpdateStoreListing updates product_url, price_hint, currency, and in_stock.
func UpdateStoreListing(l *StoreListing) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := DB.Exec(`
		UPDATE store_listings SET product_url=?, price_hint=?, currency=?, in_stock=?, updated_at=?
		WHERE id=?`,
		l.ProductURL, l.PriceHint, l.Currency, boolToInt(l.InStock), now, l.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteStoreListing removes a listing.
func DeleteStoreListing(id string) error {
	res, err := DB.Exec(`DELETE FROM store_listings WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListRedirectLog returns redirect clicks with optional filters.
// Pass empty string to skip a filter.
func ListRedirectLog(listingID, country, from, until string, limit int) ([]*RedirectLog, error) {
	query := `SELECT id, listing_id, user_id, ip_country, clicked_at FROM store_redirect_log WHERE 1=1`
	args := []any{}
	if listingID != "" {
		query += ` AND listing_id=?`
		args = append(args, listingID)
	}
	if country != "" {
		query += ` AND ip_country=?`
		args = append(args, country)
	}
	if from != "" {
		query += ` AND clicked_at>=?`
		args = append(args, from)
	}
	if until != "" {
		query += ` AND clicked_at<=?`
		args = append(args, until)
	}
	query += ` ORDER BY clicked_at DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []*RedirectLog{}
	for rows.Next() {
		r := &RedirectLog{}
		var clickedStr string
		if err := rows.Scan(&r.ID, &r.ListingID, &r.UserID, &r.IPCountry, &clickedStr); err != nil {
			return nil, err
		}
		r.ClickedAt, _ = time.Parse(time.RFC3339, clickedStr)
		logs = append(logs, r)
	}
	return logs, rows.Err()
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

func scanComponents(rows *sql.Rows) ([]*Component, error) {
	cs := []*Component{}
	for rows.Next() {
		c := &Component{}
		var createdStr, updatedStr string
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.DatasheetURL, &c.ImageURL, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		cs = append(cs, c)
	}
	return cs, rows.Err()
}

func scanBlackBoxComponents(rows *sql.Rows) ([]*BlackBoxComponent, error) {
	bs := []*BlackBoxComponent{}
	for rows.Next() {
		b := &BlackBoxComponent{}
		var createdStr string
		if err := rows.Scan(&b.ID, &b.BlackBoxName, &b.ComponentID, &b.Quantity, &b.Notes, &createdStr); err != nil {
			return nil, err
		}
		b.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		bs = append(bs, b)
	}
	return bs, rows.Err()
}

func scanStores(rows *sql.Rows) ([]*Store, error) {
	ss := []*Store{}
	for rows.Next() {
		s := &Store{}
		var activeInt int
		var createdStr, updatedStr string
		if err := rows.Scan(&s.ID, &s.Name, &s.CountryCode, &s.BaseURL, &s.AffiliateTag, &activeInt, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		s.Active = activeInt == 1
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		ss = append(ss, s)
	}
	return ss, rows.Err()
}

func scanListings(rows *sql.Rows) ([]*StoreListing, error) {
	ls := []*StoreListing{}
	for rows.Next() {
		l := &StoreListing{}
		var inStockInt int
		var updatedStr string
		if err := rows.Scan(&l.ID, &l.ComponentID, &l.StoreID, &l.ProductURL,
			&l.PriceHint, &l.Currency, &inStockInt, &updatedStr,
			&l.StoreName, &l.StoreBaseURL); err != nil {
			return nil, err
		}
		l.InStock = inStockInt == 1
		l.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		ls = append(ls, l)
	}
	return ls, rows.Err()
}

// sortBOMByName sorts BOM entries alphabetically by component name.
func sortBOMByName(entries []*BOMEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].Component.Name < entries[j-1].Component.Name; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// Note: call sites in this file do not use newID() because ID generation
// belongs to the handler layer (cryptoauth.MustNewID()), following the same
// pattern as projectapi and other handlers in this codebase.
// Store functions receive fully-formed structs with IDs already set.
