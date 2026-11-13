package store

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/epc"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
)

// The instance's RFID settings (docs/rfid.md): how asset codes are written
// to long-range tags, and read back from them.
const (
	RFIDSchemeSetting        = "rfid.scheme"
	RFIDCompanyPrefixSetting = "rfid.company_prefix"
	RFIDFilterSetting        = "rfid.filter"
	RFIDCodeTypeSetting      = "rfid.code_type"
)

// RFIDSettings are how asset codes are written to long-range RFID tags.
type RFIDSettings struct {
	// Scheme is one of epc.Schemes.
	Scheme string `json:"scheme"`
	// CompanyPrefix is the GS1 company prefix, for GIAI.
	CompanyPrefix string `json:"company_prefix"`
	// Filter is the GIAI filter value, 0 to 7.
	Filter int `json:"filter"`
	// CodeType is the type of code a tag holds, such as qr; "" is the
	// asset's first code, as labels show it.
	CodeType string `json:"code_type"`
}

// EPC is the scheme the settings describe.
func (r RFIDSettings) EPC() epc.Scheme {
	return epc.Scheme{Kind: r.Scheme, CompanyPrefix: r.CompanyPrefix, Filter: r.Filter}
}

// RFIDSettings reads the instance's RFID settings.
func (s *Store) RFIDSettings() (RFIDSettings, error) {
	out := RFIDSettings{Scheme: epc.SchemeText}
	for key, dst := range map[string]*string{
		RFIDSchemeSetting:        &out.Scheme,
		RFIDCompanyPrefixSetting: &out.CompanyPrefix,
		RFIDCodeTypeSetting:      &out.CodeType,
	} {
		v, err := s.Setting(key, *dst)
		if err != nil {
			return out, err
		}
		*dst = v
	}
	v, err := s.Setting(RFIDFilterSetting, "0")
	if err != nil {
		return out, err
	}
	out.Filter, _ = strconv.Atoi(v)
	return out, nil
}

// SetRFIDSettings saves the instance's RFID settings.
func (s *Store) SetRFIDSettings(in RFIDSettings) error {
	if in.Scheme == epc.SchemeText {
		// Only GIAI has these.
		in.CompanyPrefix, in.Filter = "", 0
	}
	if err := in.EPC().Check(); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidSetting, strings.TrimPrefix(err.Error(), epc.ErrInvalid.Error()+": "))
	}
	if in.CodeType != "" && !generators.IsValidGeneratorType(in.CodeType) {
		return fmt.Errorf("%w: the code type must be one of %v, or empty for an asset's first code", ErrInvalidSetting, generators.AllowedGeneratorTypes())
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	for key, v := range map[string]string{
		RFIDSchemeSetting:        in.Scheme,
		RFIDCompanyPrefixSetting: in.CompanyPrefix,
		RFIDFilterSetting:        strconv.Itoa(in.Filter),
		RFIDCodeTypeSetting:      in.CodeType,
	} {
		if err := s.setSettingTx(tx, key, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}
