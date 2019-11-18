/*
 * INTEL CONFIDENTIAL
 * Copyright (2017) Intel Corporation.
 *
 * The source code contained or described herein and all documents related to the source code ("Material")
 * are owned by Intel Corporation or its suppliers or licensors. Title to the Material remains with
 * Intel Corporation or its suppliers and licensors. The Material may contain trade secrets and proprietary
 * and confidential information of Intel Corporation and its suppliers and licensors, and is protected by
 * worldwide copyright and trade secret laws and treaty provisions. No part of the Material may be used,
 * copied, reproduced, modified, published, uploaded, posted, transmitted, distributed, or disclosed in
 * any way without Intel/'s prior express written permission.
 * No license under any patent, copyright, trade secret or other intellectual property right is granted
 * to or conferred upon you by disclosure or delivery of the Materials, either expressly, by implication,
 * inducement, estoppel or otherwise. Any license under such intellectual property rights must be express
 * and approved by Intel in writing.
 * Unless otherwise agreed by Intel in writing, you may not remove or alter this notice or any other
 * notice embedded in Materials by Intel or Intel's suppliers or licensors in any way.
 */

package tag

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.impcloud.net/RSP-Inventory-Suite/inventory-service/app/config"
	"github.impcloud.net/RSP-Inventory-Suite/inventory-service/pkg/encodingscheme"
	"github.impcloud.net/RSP-Inventory-Suite/inventory-service/pkg/integrationtest"
	"github.impcloud.net/RSP-Inventory-Suite/inventory-service/pkg/web"
	"math/big"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var testEpc = "3014186A343E214000000009"

var dbHost integrationtest.DBHost

func TestMain(m *testing.M) {
	dbHost = integrationtest.InitHost("tag_test")
	os.Exit(m.Run())
}

// nolint :dupl
func TestDelete(t *testing.T) {
	db := dbHost.CreateDB(t)
	defer db.Close()

	// have to insert something before we can delete it
	insertSample(t, db)

	selectQuery := fmt.Sprintf(`DELETE FROM %s`,
		pq.QuoteIdentifier(tagsTable),
	)

	_, err := db.Exec(selectQuery)
	if err != nil {
		if err == web.ErrNotFound {
			t.Fatal("Tag Not found, nothing to delete")
		}
		t.Error("Unable to delete tag", err)
	}
}

//nolint:dupl
func TestNoDataRetrieve(t *testing.T) {

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	clearAllData(t, masterDb)

	testURL, err := url.Parse("http://localhost/test?$top=10&$select=name,age")
	if err != nil {
		t.Error("failed to parse test url")
	}

	_, _, err = Retrieve(masterDb, testURL.Query(), config.AppConfig.ResponseLimit)
	if err != nil {
		t.Error("Unable to retrieve tags")
	}
}

func TestWithDataRetrieve(t *testing.T) {

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	clearAllData(t, masterDb)

	insertSample(t, masterDb)

	testURL, err := url.Parse("http://localhost/test?$top=10")
	if err != nil {
		t.Error("failed to parse test url")
	}

	tags, _, err := Retrieve(masterDb, testURL.Query(), config.AppConfig.ResponseLimit)

	if err != nil {
		t.Error("Unable to retrieve tags")
	}

	//if paging.Cursor == "" {
	//	t.Error("Cursor is empty")
	//}

	tagSlice := reflect.ValueOf(tags)

	if tagSlice.Len() <= 0 {
		t.Error("Unable to retrieve tags")
	}
}

/*func TestCursor(t *testing.T) {

	masterDb := dbTestSetup(t)
	defer masterDb.Close()

	clearAllData(t, masterDb)
	insertSample(t, masterDb)

	testURL, err := url.Parse("http://localhost/test?$top=10")
	if err != nil {
		t.Error("failed to parse test url")
	}

	tags, _, pagingfirst, err := Retrieve(masterDb, testURL.Query(), config.AppConfig.ResponseLimit)

	if err != nil {
		t.Error("Unable to retrieve tags")
	}

	if pagingfirst.Cursor == "" {
		t.Error("Cursor is empty")
	}

	cFirst := pagingfirst.Cursor
	tagSlice := reflect.ValueOf(tags)

	if tagSlice.Len() <= 0 {
		t.Error("Unable to retrieve tags")
	}

	// Initiating second http request to check if first sceond cursor or not same
	insertSampleCustom(t, masterDb, "cursor")

	cursorTestURL, err := url.Parse("http://localhost/test?$filter=_id gt '" + url.QueryEscape(cFirst) + "'&$top=10")
	if err != nil {
		t.Error("failed to parse test url")
	}

	ctags, _, pagingnext, err := Retrieve(masterDb, cursorTestURL.Query(), config.AppConfig.ResponseLimit)

	if err != nil {
		t.Error("Unable to retrieve tags")
		fmt.Println(err.Error())
	}

	if pagingnext.Cursor == "" {
		t.Error("Cursor is empty")
	}

	cSecond := pagingnext.Cursor
	tagSlice2 := reflect.ValueOf(ctags)

	if tagSlice2.Len() <= 0 {
		t.Error("Unable to retrieve tags")
	}

	if cFirst == cSecond {
		t.Error("paging failed ")
	}

}*/

//nolint:dupl
func TestRetrieveCount(t *testing.T) {
	testCases := []string{
		"http://localhost/test?$count",
		"http://localhost/test?$count&$filter=startswith(epc,'3')",
	}

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	for _, item := range testCases {
		testURL, err := url.Parse(item)
		if err != nil {
			t.Error("failed to parse test url")
		}

		retrieveCountTest(t, testURL, masterDb)
	}
}

func retrieveCountTest(t *testing.T, testURL *url.URL, session *sql.DB) {
	results, count, err := Retrieve(session, testURL.Query(), config.AppConfig.ResponseLimit)
	if results != nil {
		t.Error("expecting results to be nil")
	}
	if count == nil {
		t.Error("expecting CountType result")
	}
	if err != nil {
		t.Error("Unable to retrieve total count")
	}
}

func TestRetrieveInlineCount(t *testing.T) {

	testURL, err := url.Parse("http://localhost/test?$filter=test eq 1&$inlinecount=allpages")
	if err != nil {
		t.Error("failed to parse test url")
	}

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	results, count, err := Retrieve(masterDb, testURL.Query(), config.AppConfig.ResponseLimit)

	if results == nil {
		t.Error("expecting results to not be nil")
	}

	if count == nil {
		t.Error("expecting inlinecount result")
	}

	if err != nil {
		t.Error("Unable to retrieve", err.Error())
	}
}

func TestRetrieveOdataAllWithOdataQuery(t *testing.T) {

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	tagArray := make([]Tag, 2)

	var tag0 Tag
	tag0.Epc = "303401D6A415B5C000000002"
	tag0.FacilityID = "facility1"
	tag0.URI = "tag1.test"
	tagArray[0] = tag0

	var tag1 Tag
	tag1.Epc = "303401D6A415B5C000000001"
	tag1.FacilityID = "facility2"
	tag1.URI = "tag2.test"
	tagArray[1] = tag1

	err := Replace(masterDb, tagArray)
	if err != nil {
		t.Error("Unable to insert tags", err.Error())
	}

	odataMap := make(map[string][]string)
	odataMap["$filter"] = append(odataMap["$filter"], "facility_id eq facility1")

	//tags, err := RetrieveOdataAll(masterDb, odataMap)
	tags, err := RetrieveOdataAll(masterDb, odataMap)
	if err != nil {
		t.Error("Error in retrieving tags based on odata query")
	} else {
		tagSlice, err := unmarshallTagsInterface(tags)
		if err != nil {
			t.Error("Error in unmarshalling tag interface")
		}
		if len(tagSlice) != 1 {
			t.Error("Expected one tag to be retrieved based on query", len(tagSlice))
		}
	}
	clearAllData(t, masterDb)
}

func unmarshallTagsInterface(tags interface{}) ([]Tag, error) {

	tagsBytes, err := json.Marshal(tags)
	if err != nil {
		return nil, errors.Wrap(err, "marshaling []interface{} to []bytes")
	}

	var tagSlice []Tag
	if err := json.Unmarshal(tagsBytes, &tagSlice); err != nil {
		return nil, errors.Wrap(err, "unmarshaling []bytes to []Tags")
	}

	return tagSlice, nil
}

func TestRetrieveOdataAllNoOdataQuery(t *testing.T) {

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	clearAllData(t, masterDb)

	numOfSamples := 600
	tagSlice := make([]Tag, numOfSamples)
	epcSlice := generateSequentialEpcs("3014", 0, int64(numOfSamples))

	for i := 0; i < numOfSamples; i++ {
		var tag Tag
		tag.Epc = epcSlice[i]
		tag.Source = "fixed"
		tag.Event = "arrived"
		tag.URI = "test" + "." + epcSlice[i]
		tagSlice[i] = tag
	}

	err := Replace(masterDb, tagSlice)
	if err != nil {
		t.Errorf("Unable to insert tags in bulk: %s", err.Error())
	}

	odataMap := make(map[string][]string)

	tags, err := RetrieveOdataAll(masterDb, odataMap)
	if err != nil {
		t.Error("Error in retrieving tags")
	} else {
		tagSlice, err := unmarshallTagsInterface(tags)
		if err != nil {
			t.Error("Error in unmarshalling tag interface")
		}
		if len(tagSlice) != numOfSamples {
			t.Error("Number of tags in database and number of tags retrieved do not match")
		}
	}
	clearAllData(t, masterDb)
}

func TestInsert(t *testing.T) {
	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	insertSample(t, masterDb)
}

func TestDataReplace(t *testing.T) {

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	tagArray := make([]Tag, 2)

	var tag0 Tag
	tag0.Epc = testEpc
	tag0.URI = "tag1.test"
	tag0.Tid = t.Name() + "0"
	tag0.Source = "fixed"
	tag0.Event = "arrived"
	tagArray[0] = tag0

	var tag1 Tag
	tag1.Epc = "303401D6A415B5C000000001"
	tag1.URI = "tag2.test"
	tag1.Tid = t.Name() + "1"
	tag1.Source = "handheld"
	tag1.Event = "arrived"
	tagArray[1] = tag1

	err := Replace(masterDb, tagArray)
	if err != nil {
		t.Error("Unable to replace tags", err.Error())
	}
	clearAllData(t, masterDb)
}

func TestRetrieveSizeLimitWithTop(t *testing.T) {

	var sizeLimit = 1

	// Trying to return more than 1 result
	testURL, err := url.Parse("http://localhost/test?$inlinecount=allpages&$top=2")
	if err != nil {
		t.Error("Failed to parse test URL")
	}

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	numOfSamples := 10

	tagSlice := make([]Tag, numOfSamples)

	epcSlice := generateSequentialEpcs("3014", 0, int64(numOfSamples))

	for i := 0; i < numOfSamples; i++ {
		var tag Tag
		tag.Epc = epcSlice[i]
		tag.URI = "test" + "." + epcSlice[i]
		tag.Source = "fixed"
		tag.Event = "arrived"
		tagSlice[i] = tag
	}

	if replaceErr := Replace(masterDb, tagSlice); replaceErr != nil {
		t.Errorf("Unable to replace tags: %s", replaceErr.Error())
	}

	results, count, err := Retrieve(masterDb, testURL.Query(), sizeLimit)
	if err != nil {
		t.Errorf("Retrieve failed with error %v", err.Error())
	}

	resultSlice := reflect.ValueOf(results)

	if resultSlice.Len() > sizeLimit {
		t.Errorf("Error retrieving results with size limit. Expected: %d , received: %d", sizeLimit, count.Count)
	}
	clearAllData(t, masterDb)
}

func TestRetrieveSizeLimitInvalidTop(t *testing.T) {

	var sizeLimit = 1

	// Trying to return more than 1 result
	testURL, err := url.Parse("http://localhost/test?$inlinecount=allpages&$top=string")
	if err != nil {
		t.Error("Failed to parse test URL")
	}

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	_, _, err = Retrieve(masterDb, testURL.Query(), sizeLimit)
	if err == nil {
		t.Errorf("Expecting an error for invalid $top value")
	}

}

func TestDataReplace_Bulk(t *testing.T) {

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	numOfSamples := 600

	tagSlice := make([]Tag, numOfSamples)

	epcSlice := generateSequentialEpcs("3014", 0, int64(numOfSamples))

	for i := 0; i < numOfSamples; i++ {
		var tag Tag
		tag.Epc = epcSlice[i]
		tag.URI = "test" + "." + epcSlice[i]
		tag.Source = "fixed"
		tag.Event = "arrived"
		tagSlice[i] = tag
	}

	err := Replace(masterDb, tagSlice)
	if err != nil {
		t.Errorf("Unable to replace tags: %s", err.Error())
	}

	// randomly pick one to test to save testing time
	indBig, randErr := rand.Int(rand.Reader, big.NewInt(int64(numOfSamples)))
	var testIndex int
	if randErr != nil {
		testIndex = 0
	} else {
		testIndex = int(indBig.Int64())
	}
	epcToTest := epcSlice[testIndex]
	gotTag, err := FindByEpc(masterDb, epcToTest)

	if err != nil {
		t.Error("Unable to retrieve tags")
	}

	if !gotTag.IsTagReadByRspController() {
		t.Error("Unable to retrieve tags")
	}
	clearAllData(t, masterDb)
}

// nolint :dupl
func TestDeleteTagCollection(t *testing.T) {

	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	// have to insert something before we can delete it
	insertSample(t, masterDb)

	if err := DeleteTagCollection(masterDb); err != nil {
		if err == web.ErrNotFound {
			t.Fatal("Tag Not found, nothing to delete")
		}
		t.Error("Unable to delete tag")
	}
}

//nolint:dupl
func TestDelete_nonExistItem(t *testing.T) {
	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	// we will try to delete random gibberish
	if err := Delete(masterDb, "emptyId"); err != nil {
		if err == web.ErrNotFound {
			// because we didn't find it, it should succeed
			t.Log("Tag NOT FOUND, this is the expected result")
		} else {
			t.Error("Expected to not be able to delete")
		}
	}
}

func TestUpdate(t *testing.T) {
	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	epc := "30143639F8419105417AED6F"
	facilityID := "TestFacility"
	// insert sample data
	var tagArray = []Tag{
		{
			Epc:        epc,
			FacilityID: facilityID,
		},
	}

	err := Replace(masterDb, tagArray)
	if err != nil {
		t.Error("Unable to insert tag", err.Error())
	}

	objectMap := make(map[string]string)
	objectMap["qualified_state"] = "sold"

	err = Update(masterDb, epc, facilityID, objectMap)
	if err != nil {
		t.Error("Unable to update the tag", err.Error())
	}

	// verify that update was successful
	tag, err := FindByEpc(masterDb, epc)
	if err != nil {
		t.Errorf("Error trying to find tag by epc %s", err.Error())
	} else if !tag.IsTagReadByRspController() {
		if tag.QualifiedState != "sold" {
			t.Fatal("Qualified_state update failed")
		}
	}

	//clean data
	clearAllData(t, masterDb)
	err = Update(masterDb, epc, facilityID, objectMap)
	if err == nil {
		t.Error("Tag not found error not caught")
	}
}

func TestFindByEpc_found(t *testing.T) {
	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	epc := t.Name()
	insertSampleCustom(t, masterDb, epc)

	tag, err := FindByEpc(masterDb, epc)
	if err != nil {
		t.Errorf("Error trying to find tag by epc %s", err.Error())
	} else if !tag.IsTagReadByRspController() {
		t.Errorf("Expected to find a tag with epc: %s", epc)
	} else if tag.Epc != epc {
		t.Error("Expected found tag epc to be equal to the input epc")
	}

	selectQuery := fmt.Sprintf(`DELETE FROM %s`,
		pq.QuoteIdentifier(tagsTable),
	)

	_, err = masterDb.Exec(selectQuery)
	if err != nil {
		if err == web.ErrNotFound {
			t.Fatal("Tag Not found, nothing to delete")
		}
		t.Error("Unable to delete tag", err)
	}
	clearAllData(t, masterDb)
}

func TestFindByEpc_notFound(t *testing.T) {
	masterDb := dbHost.CreateDB(t)
	defer masterDb.Close()

	epc := t.Name()
	tag, err := FindByEpc(masterDb, epc)
	if err != nil {
		t.Errorf("Error trying to find tag by epc %s", err.Error())
	} else if tag.IsTagReadByRspController() {
		t.Errorf("Expected to NOT find a tag with epc: %s", epc)
	}
}

func TestCalculateGtin(t *testing.T) {
	config.AppConfig.TagDecoders = []encodingscheme.TagDecoder{encodingscheme.NewSGTINDecoder(true)}
	validEpc := "303402662C3A5F904C19939D"
	gtin, _, err := DecodeTagData(validEpc)
	if gtin == UndefinedProductID {
		t.Errorf("Error trying to calculate valid epc %s: %+v", validEpc, err)
	}
}

func TestCalculateInvalidGtin(t *testing.T) {
	setSGTINOnlyDecoderConfig()
	epcSlice := generateSequentialEpcs("0014", 0, 1)
	gtin, _, err := DecodeTagData(epcSlice[0])
	if gtin != encodingInvalid {
		t.Errorf("Unexpected result calculating invalid epc %s, expected %s, got %s. error val was: %+v",
			epcSlice[0], UndefinedProductID, gtin, err)
	}
}

func setSGTINOnlyDecoderConfig() {
	config.AppConfig.TagDecoders = []encodingscheme.TagDecoder{
		encodingscheme.NewSGTINDecoder(true),
	}
}

func setMixedDecoderConfig(t *testing.T) {
	decoder, err := encodingscheme.NewProprietary(
		"test.com", "2019-01-01", "8.48.40", 2)
	if err != nil {
		t.Fatal(err)
	}
	config.AppConfig.TagDecoders = []encodingscheme.TagDecoder{
		encodingscheme.NewSGTINDecoder(true),
		decoder,
	}
}

func TestCalculateProductCode(t *testing.T) {
	setMixedDecoderConfig(t)
	validEpc := "0F00000000000C00000014D2"
	expectedWrin := "00000014D2"
	productID, _, err := DecodeTagData(validEpc)
	if productID == UndefinedProductID {
		t.Errorf("Error trying to calculate valid epc %s: %+v", validEpc, err)
	}
	if productID != expectedWrin {
		t.Errorf("Error trying to calculate valid epc %s: wanted %s, got %s; err is: %+v",
			validEpc, expectedWrin, productID, err)
	}
}

func insertSample(t *testing.T, db *sql.DB) {
	insertSampleCustom(t, db, t.Name())
}

func insertSampleCustom(t *testing.T, db *sql.DB, sampleID string) {
	var tag Tag

	tag.Epc = sampleID

	if err := insert(db, tag); err != nil {
		t.Error("Unable to insert tag", err)
	}
}

//
//// nolint :dupl
func clearAllData(t *testing.T, db *sql.DB) {
	selectQuery := fmt.Sprintf(`DELETE FROM %s`,
		pq.QuoteIdentifier(tagsTable),
	)

	_, err := db.Exec(selectQuery)
	if err != nil {
		t.Errorf("Unable to delete data from %s table: %s", tagsTable, err)
	}
}

// nolint :dupl
func insert(dbs *sql.DB, tag Tag) error {

	obj, err := json.Marshal(tag)
	if err != nil {
		return err
	}

	upsertStmt := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) 
									 ON CONFLICT (( %s  ->> 'epc' )) 
									 DO UPDATE SET %s = %s.%s || %s; `,
		pq.QuoteIdentifier(tagsTable),
		pq.QuoteIdentifier(jsonb),
		pq.QuoteLiteral(string(obj)),
		pq.QuoteIdentifier(jsonb),
		pq.QuoteIdentifier(jsonb),
		pq.QuoteIdentifier(tagsTable),
		pq.QuoteIdentifier(jsonb),
		pq.QuoteLiteral(string(obj)),
	)

	_, err = dbs.Exec(upsertStmt)
	if err != nil {
		return errors.Wrap(err, "error in inserting tag")
	}
	return nil
}

//nolint:unparam
func generateSequentialEpcs(header string, offset int64, limit int64) []string {
	digits := 24 - len(header)
	epcs := make([]string, limit)
	for i := int64(0); i < limit; i++ {
		epcs[i] = strings.ToUpper(fmt.Sprintf("%s%0"+strconv.Itoa(digits)+"s", header, strconv.FormatInt(offset+i, 16)))
	}
	return epcs
}
