package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
)

const PREFIX_REGEX = `^([0-9]+)-([0-9A-Za-z]+)_`

type Item struct {
	name string
	paid decimal.Decimal
}

type EOB struct {
	checkTotal  decimal.Decimal
	checkNumber string
	checkFile   string
	items       []Item
}

type EOBErrors []string

func main() {
	total := flag.Float64("total", 0.00, "The expected total for this set of eobs")
	targetDir := flag.String("dir", "storage", "The target dir to parse the set of eobs")
	flag.Parse()

	if *total == 0.00 {
		fmt.Println("--total is required. For help: --help")
		os.Exit(0)
	}

	parse(*targetDir, decimal.NewFromFloat(*total))
}

type EOBType map[string]EOB

// eobIsInit checks if the EOB has already been initialized
// returns the EOBs map key
func (eobs EOBType) eobIsInit(prefix string) (string, bool) {
	for k, _ := range eobs {
		if prefix == k {
			return k, true
		}
	}
	return "", false
}

// checkIntegrity checks for errors, stores it and reports
func (eobs EOBType) checkIntegrity() (decimal.Decimal, EOBErrors) {
	var errors EOBErrors

	setTotal, _ := decimal.NewFromString("0.00")

	for set, eob := range eobs {
		// verify if there is a check amount
		if eob.checkTotal.LessThanOrEqual(decimal.NewFromInt(0)) {
			msg := fmt.Sprintf("There is no check total for %s\n", set)
			errors = append(errors, msg)
		}
		// verify check number
		if eob.checkNumber == "" {
			msg := fmt.Sprintf("Check number does not exist for %s\n", set)
			errors = append(errors, msg)
		}
		if eob.checkFile == "" {
			msg := fmt.Sprintf("Missing check file for %s\n", set)
			errors = append(errors, msg)
		} else {
			// check that check file matches total and checknumber
			chkSplit := strings.Split(eob.checkFile, "-")
			chkTotal, _ := decimal.NewFromString(currencyFormat(chkSplit[0]))
			if !chkTotal.Equals(eob.checkTotal) {
				msg := fmt.Sprintf("Check total does not match between set %s and file %s\n", set, eob.checkFile)
				errors = append(errors, msg)
			}
			// verify that check number matches
			chkNum := strings.Split(chkSplit[1], "_")[0]
			if chkNum != eob.checkNumber {
				msg := fmt.Sprintf("Check number does not match for file %s\n", eob.checkNumber)
				errors = append(errors, msg)
			}
		}
		// verify the total of the EOBs matches the check total
		eobTotal, _ := decimal.NewFromString("0.00")
		for _, eob := range eob.items {
			eobTotal = eobTotal.Add(eob.paid)
		}
		if !eobTotal.Equals(eob.checkTotal) {
			msg := fmt.Sprintf("Check total %v does not match item totals %v for %s\n", eob.checkTotal, eobTotal, set)
			errors = append(errors, msg)
		}

		setTotal = setTotal.Add(eobTotal)
	}

	return setTotal, errors
}

func parse(targetDir string, expTotal decimal.Decimal) {
	dir, err := os.ReadDir(targetDir)
	if err != nil {
		log.Fatalln(err)
	}

	// init the EOBs var
	EOBs := make(EOBType)

	for _, file := range dir {

		// skip files that does not conform
		compile, err := regexp.Compile(PREFIX_REGEX)
		matches := compile.FindStringSubmatch(file.Name())
		if len(matches) == 0 {
			continue
		}

		ps, err := parsePreAndSuf(file.Name())
		if err != nil {
			log.Fatalln(err)
		}

		// check if we already initialized this EOB
		key, init := EOBs.eobIsInit(ps.prefix)
		if !init { // not initialized yet so initialize
			// extract the check total and check number
			prefix := strings.Split(ps.prefix, "-")

			if len(prefix) != 2 {
				log.Fatalln("Wrong prefix and suffix for", file.Name())
			}

			chkTotal, err := decimal.NewFromString(currencyFormat(prefix[0]))
			if err != nil {
				log.Fatalln(err)
			}

			newEob := EOB{
				checkTotal:  chkTotal,
				checkNumber: prefix[1],
			}

			if ps.suffix == "check" {
				newEob.checkFile = ps.suffix
			}

			key = ps.prefix
			EOBs[key] = newEob
		}

		if ps.suffix == "check" {
			if entry, ok := EOBs[key]; ok {
				entry.checkFile = file.Name()
				EOBs[key] = entry // we need to reassign entry. ugh
			}
		} else {
			// this should be the EOBs
			eobStrings := strings.Split(ps.suffix, "_")
			newItem := Item{}
			for i, val := range eobStrings {
				if (i+1)%2 == 0 { // this is the currency payment

					newItem.paid, err = decimal.NewFromString(currencyFormat(val))
					if err != nil {
						log.Fatalln(err)
					}

					if entry, ok := EOBs[key]; ok {
						entry.items = append(entry.items, newItem)
						EOBs[key] = entry
					}

					// reset newItem
					newItem = Item{}

				} else { // this is the fullname
					newItem.name = val
				}
			}
		}
	}

	//verification that total equals and the checkTotal equals the sum of the paid items
	errNo := 0
	setTotal, errors := EOBs.checkIntegrity()

	if !setTotal.Equal(expTotal) {
		errNo++
		fmt.Printf("%d. The expected total of %v does not equal %v\n", errNo, expTotal, setTotal)
	}
	for _, err := range errors {
		errNo++
		fmt.Printf("%d. %s", errNo, err)
	}

	if errNo == 0 {
		fmt.Println("No errors found")
	}
}

type PreSuf struct {
	prefix string
	suffix string
}

func parsePreAndSuf(filename string) (*PreSuf, error) {
	compile, err := regexp.Compile(PREFIX_REGEX)
	if err != nil {
		return nil, fmt.Errorf("Incorrect prefix for file:%s", filename)
	}
	reg := compile.FindStringSubmatch(filename)

	prefix := reg[0]
	suffix := filename[len(prefix):]

	ps := PreSuf{
		prefix: prefix[0 : len(prefix)-1], // trim the "_"
	}

	// determine if this is a "check" file or EOB
	if string(suffix[0:3]) == "EOB" { // an EOB structure
		ps.suffix = suffix[4 : len(suffix)-4] // remove the file extension
	} else if string(suffix[0:5]) == "check" {
		ps.suffix = suffix[0 : len(suffix)-4] // remove the file extension
	}

	return &ps, nil
}

func currencyFormat(s string) string {
	dollars := s[0 : len(s)-2]
	cents := s[len(s)-2:]

	return dollars + "." + cents
}
