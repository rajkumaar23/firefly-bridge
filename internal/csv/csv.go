package csv

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/sirupsen/logrus"
	"github.com/xuri/excelize/v2"
)

// FieldConfig defines the mapping of transaction fields to CSV columns. Column indices are 1-based.
// Amount can be specified either as a single column or split into debit and credit columns.
type FieldConfig struct {
	Date struct {
		Column int    `yaml:"column" validate:"required"`
		Format string `yaml:"format" validate:"required"`
	} `yaml:"date" validate:"required"`
	Description struct {
		Column int `yaml:"column" validate:"required"`
	} `yaml:"description" validate:"required"`
	Category struct {
		Column int `yaml:"column" validate:"omitempty,gt=0"`
	} `yaml:"category"`
	Notes struct {
		Column int `yaml:"column" validate:"omitempty,gt=0"`
	} `yaml:"notes"`
	Amount struct {
		Column int  `yaml:"column" validate:"omitempty,gt=0"`
		Negate bool `yaml:"negate"`
	} `yaml:"amount"`
	Debit struct {
		Column int  `yaml:"column" validate:"omitempty,gt=0"`
		Negate bool `yaml:"negate"`
	} `yaml:"debit"`
	Credit struct {
		Column int  `yaml:"column" validate:"omitempty,gt=0"`
		Negate bool `yaml:"negate"`
	} `yaml:"credit"`
}

// Validate() is automatically called by the validator when the struct is initialized in chromedp GetTransactionsStep.
// This happens because of the `validate:"validateFn"` tag on the Config field.
func (f *FieldConfig) Validate() error {
	hasAmount := f.Amount.Column > 0
	hasDebit := f.Debit.Column > 0
	hasCredit := f.Credit.Column > 0

	if !hasAmount && (!hasDebit || !hasCredit) {
		return fmt.Errorf("must specify either 'amount' or both 'debit' and 'credit'")
	}

	if hasAmount && (hasDebit || hasCredit) {
		return fmt.Errorf("cannot specify both 'amount' and 'debit/credit'")
	}

	if (hasDebit && !hasCredit) || (!hasDebit && hasCredit) {
		return fmt.Errorf("'debit' and 'credit' must be specified together")
	}

	return nil
}

// Options defines additional settings for parsing CSV files
type Options struct {
	Delimiter    string `yaml:"delimiter"`
	SkipHeadRows int    `yaml:"skip_head_rows"`
	SkipTailRows int    `yaml:"skip_tail_rows"`
	//TODO: add SkipRowConditions to allow skipping rows based on custom conditions (e.g. if a specific column contains a certain value)
}

type Parser struct {
	ctx    context.Context
	logger *logrus.Logger
	opts   *Options
	config *FieldConfig
}

func NewParser(ctx context.Context, opts *Options, cfg *FieldConfig) *Parser {
	logger := utils.GetLogger(ctx)
	return &Parser{ctx: ctx, logger: logger, opts: opts, config: cfg}
}

func (p *Parser) Parse(path string) ([]firefly.TransactionSplit, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}

	defer func() {
		err := file.Close()
		if err != nil {
			p.logger.Warnf("error closing file (%s): %s", path, err.Error())
		}
		if err := os.Remove(path); err != nil {
			p.logger.Errorf("error deleting file (%s): %s", path, err.Error())
		}
	}()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	if p.opts.Delimiter != "" {
		reader.Comma = rune(p.opts.Delimiter[0])
	}
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("error reading csv file: %w", err)
	}

	if p.opts.SkipHeadRows > len(records) {
		p.logger.Warnf("no records to process in file: %s", path)
		return nil, nil
	}
	if p.opts.SkipHeadRows > 0 {
		records = records[p.opts.SkipHeadRows:]
	}
	if p.opts.SkipTailRows > 0 {
		records = records[:len(records)-p.opts.SkipTailRows]
	}

	var transactions []firefly.TransactionSplit
	for idx, record := range records {
		description, err := p.getDescription(record)
		if err != nil {
			return nil, fmt.Errorf("error parsing description for file '%s' (idx=%d): %w", path, idx, err)
		}
		date, err := p.getDate(record)
		if err != nil {
			return nil, fmt.Errorf("error parsing date for file '%s' (idx=%d): %w", path, idx, err)
		}
		amount, err := p.getAmount(record)
		if err != nil {
			return nil, fmt.Errorf("error parsing amount for file '%s' (idx=%d): %w", path, idx, err)
		}
		category, err := p.getCategory(record)
		if err != nil {
			return nil, fmt.Errorf("error parsing category for file '%s' (idx=%d): %w", path, idx, err)
		}
		notes, err := p.getNotes(record)
		if err != nil {
			return nil, fmt.Errorf("error parsing notes for file '%s' (idx=%d): %w", path, idx, err)
		}

		transaction := &firefly.TransactionSplit{
			Date:         date,
			Amount:       strconv.FormatFloat(math.Abs(amount), 'f', 2, 64),
			Description:  description,
			CategoryName: &category,
			Notes:        &notes,
		}

		if amount < 0 {
			transaction.Type = "withdrawal"
		} else {
			transaction.Type = "deposit"
		}

		transactions = append(transactions, *transaction)
	}

	return transactions, nil
}

func (p *Parser) ParseFromExcel(path string, worksheetIndex int) ([]firefly.TransactionSplit, error) {
	xlFile := p.getExcelFile(path)

	worksheets := xlFile.GetSheetList()
	if len(worksheets) < 1 {
		return nil, fmt.Errorf("no worksheets found in the file")
	}

	csvPath, err := p.createCSVFile(xlFile, worksheets[worksheetIndex])
	if err != nil {
		return nil, fmt.Errorf("error creating csv file: %w", err)
	}

	if err := os.Remove(path); err != nil {
		p.logger.Errorf("error deleting excel file: %s", err.Error())
	}

	return p.Parse(csvPath)
}

// Helper methods to extract fields from CSV records with error handling and validation
func (p *Parser) getDescription(record []string) (string, error) {
	if p.config.Description.Column <= 0 || p.config.Description.Column > len(record) {
		return "", fmt.Errorf("description column index out of bounds")
	}
	return strings.TrimSpace(record[p.config.Description.Column-1]), nil
}

func (p *Parser) getDate(record []string) (time.Time, error) {
	if p.config.Date.Column <= 0 || p.config.Date.Column > len(record) {
		return time.Time{}, fmt.Errorf("date column index out of bounds")
	}
	dateStr := strings.TrimSpace(record[p.config.Date.Column-1])
	return utils.ParseLocalDateFromString(p.config.Date.Format, dateStr)
}

func (p *Parser) getAmount(record []string) (amount float64, err error) {
	if p.config.Amount.Column != 0 {
		if p.config.Amount.Column <= 0 || p.config.Amount.Column > len(record) {
			return 0.0, fmt.Errorf("amount column index out of bounds")
		}
		amountStr := record[p.config.Amount.Column-1]
		amount, err = utils.ParseAmountFromString(amountStr)
		if err != nil {
			return 0.0, err
		}

		if p.config.Amount.Negate {
			amount = -1 * amount
		}

		return amount, nil
	}

	if p.config.Debit.Column <= 0 || p.config.Debit.Column > len(record) {
		return 0.0, fmt.Errorf("debit column index out of bounds")
	}
	debitStr := record[p.config.Debit.Column-1]

	if debitStr != "" {
		amount, err = utils.ParseAmountFromString(debitStr)
		if err != nil {
			return 0.0, fmt.Errorf("error parsing debit amount: %w", err)
		}
		// For debit column, we assume that the given absolute amount is negative since it's money going out. 
		// This can be overriden by setting 'negate' to true in the config.
		if !p.config.Debit.Negate {
			amount = -1 * amount
		}
		return amount, nil
	}

	if p.config.Credit.Column <= 0 || p.config.Credit.Column > len(record) {
		return 0.0, fmt.Errorf("credit column index out of bounds")
	}
	creditStr := record[p.config.Credit.Column-1]

	amount, err = utils.ParseAmountFromString(creditStr)
	if err != nil {
		return 0.0, fmt.Errorf("error parsing credit amount: %w", err)
	}
	if p.config.Credit.Negate {
		amount = -1 * amount
	}

	return amount, nil
}

func (p *Parser) getCategory(record []string) (string, error) {
	if p.config.Category.Column == 0 {
		return "", nil
	}
	if p.config.Category.Column <= 0 || p.config.Category.Column > len(record) {
		return "", fmt.Errorf("category column index out of bounds")
	}
	return strings.TrimSpace(record[p.config.Category.Column-1]), nil
}

func (p *Parser) getNotes(record []string) (string, error) {
	if p.config.Notes.Column == 0 {
		return "", nil
	}
	if p.config.Notes.Column <= 0 || p.config.Notes.Column > len(record) {
		return "", fmt.Errorf("notes column index out of bounds")
	}
	return strings.TrimSpace(record[p.config.Notes.Column-1]), nil
}

// Helpers to convert XLS(X) files to CSV
func (p *Parser) getExcelFile(fileName string) *excelize.File {
	xlFile, xlErr := excelize.OpenFile(fileName)
	if xlErr != nil {
		panic(xlErr)
	}
	defer func() {
		if xlErr := xlFile.Close(); xlErr != nil {
			panic(xlErr)
		}
	}()

	return xlFile
}

func (p *Parser) createCSVFile(xlFile *excelize.File, worksheet string) (string, error) {
	allRows, err := xlFile.GetRows(worksheet)
	if err != nil {
		return "", fmt.Errorf("failed to get rows: %w", err)
	}

	filePath := worksheet + ".csv"
	csvFile, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("error creating csv file: %w", err)
	}

	defer func() {
		if err := csvFile.Close(); err != nil {
			logrus.WithField("file", csvFile.Name()).Warnf("error closing file: %s", err.Error())
		}
	}()

	writer := csv.NewWriter(csvFile)
	err = writer.WriteAll(allRows)
	if err != nil {
		return "", fmt.Errorf("error writing rows to csv: %w", err)
	}

	return filePath, nil
}
