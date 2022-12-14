package icp

import (
	"fmt"
	"github.com/spf13/viper"
	"github.com/xuri/excelize/v2"
	"log"
	"path/filepath"
	"strings"
	"sysafari.com/customs/tguard/global"
	"sysafari.com/customs/tguard/utils"
	"time"
)

const (
	FileNameDateLayout = "200601"
	FileNameTimeLayout = "02150405"
	FloatDecimalPlaces = 6
)

type FileOfICP struct {
	// CustomsIDs The customs IDs that needs to be placed in the ICP file
	CustomsIDs []string `json:"customs_ids"`
	// Month ICP file for which month, exp: 2006-01
	Month string `json:"month"`
	// The duty party
	DutyParty string `json:"duty_party"`
	// TaxData Population data for tax information form
	TaxData []TaxObject `json:"tax_data"`
	// TaxFileData Population data for tax file information form
	TaxFileData []TaxFileObject `json:"tax_file_data"`
	// PodFileData The data used to fill the pod file table
	PodFileData []PodFileObject `json:"pod_file_data"`
	// FilePath The full path of ICP file.
	FilePath string `json:"file_path"`
	// FileName The ICP file name
	FileName string `json:"file_name"`
	// Errors The ICP errors
	Errors []string `json:"errors"`
}

// QueryCustomsIDs Query customs IDs between the startDate and endDate
func (f *FileOfICP) QueryCustomsIDs() {
	var customsIds []string
	err := global.Db.Select(&customsIds, QueryCustomsIdForICPWithinOneMonthSql, f.DutyParty, f.Month)
	if err != nil || len(customsIds) == 0 {
		f.Errors = append(f.Errors, fmt.Sprintf("Can not query customs for duty party %s with month %s", f.DutyParty, f.Month))
	}
	log.Printf("Total cusotms: %d", len(customsIds))
	f.CustomsIDs = customsIds
}

// GenerateICP Begin to generate ICP file
func (f *FileOfICP) GenerateICP() string {
	if len(f.Errors) == 0 {
		f.readyICPFileInfo()
		if len(f.Errors) > 0 {
			log.Printf("Generating ICP ready ICP info error: %v \n", f.Errors)
		}

		f.generateFillData()
		if len(f.Errors) > 0 {
			log.Printf("Generating ICP query fill data error: %v \n", f.Errors)
		}

		f.createICPFile()
		if len(f.Errors) > 0 {
			log.Printf("Generating ICP create ICP excel file error: %v \n", f.Errors)
		}

		f.saveICPInfoIntoDB(true)
		f.saveCustomsInfoWithinICP()
		if len(f.Errors) > 0 {
			log.Printf("Save ICP and customs info failed, error: %v \n", f.Errors)
		}

		return f.FileName
	}
	return ""
}

// saveICPInfoIntoDB Save ICP info to database
func (f *FileOfICP) saveICPInfoIntoDB(status bool) {
	dt, err := time.Parse("2006-01", f.Month)
	if err != nil {
		f.Errors = append(f.Errors, fmt.Sprintf("ICP's filename(%s) error: %v", f.FileName, err))
	}
	serviceIcp := &ServiceICP{
		DutyParty: f.DutyParty,
		Name:      f.FileName,
		Year:      dt.Year(),
		Month:     int(dt.Month()),
		IcpDate:   time.Now().UTC().Format("2006-01-02 15:04:05"),
		Total:     len(f.CustomsIDs),
		Status:    status,
	}
	_, err = global.Db.NamedExec(InsertServiceICP, serviceIcp)
	if err != nil {
		f.Errors = append(f.Errors, fmt.Sprintf("Save ICP(%s) information failed: %v", f.FileName, err))
	}
}

// saveCustomsInfoWithinICP Save relations information for customs and ICP
func (f *FileOfICP) saveCustomsInfoWithinICP() {
	var customsICPs []ServiceICPCustoms

	for _, i2 := range f.TaxFileData {
		customsId := i2.CustomsId
		ci := ServiceICPCustoms{
			IcpName:   f.FileName,
			CustomsId: customsId,
			TaxType:   i2.TaxType,
			InExcel:   utils.In(customsId, f.CustomsIDs),
		}
		customsICPs = append(customsICPs, ci)
	}

	_, err := global.Db.NamedExec(InsertServiceICPCustoms, customsICPs)
	if err != nil {
		f.Errors = append(f.Errors, fmt.Sprintf("Save ICP(%s)'s customs information failed: %v", f.FileName, err))
	}

}

// generateFillData Generate fill data for ICP file.
func (f *FileOfICP) generateFillData() {
	log.Printf("**** Begin to generate ICP file ****")
	for i, d := range f.CustomsIDs {
		log.Printf("**** %d cusotms ID: %s ****", i, d)
		icp := &CustomsICP{
			CustomsId: d,
		}
		icp.QueryFillData()
		if len(icp.Errors) == 0 {
			f.TaxData = append(f.TaxData, icp.TaxData...)
			f.TaxFileData = append(f.TaxFileData, icp.TaxFileData...)
			f.PodFileData = append(f.PodFileData, icp.PodFileData...)
		} else {
			f.Errors = append(f.Errors, icp.Errors...)
		}
	}
}

// readyICPFileInfo Get ready for icp file info
func (f *FileOfICP) readyICPFileInfo() {
	saveRoot := viper.GetString("icp.save-dir")
	if saveRoot == "" {
		log.Panic("ICP root save directory not set ..")
	}

	monthDt, err := time.Parse("2006-01", f.Month)
	if err != nil {
		f.Errors = append(f.Errors, fmt.Sprintf("ICP's month format error, %s.", f.Month))
	}
	if f.FileName == "" {
		if f.DutyParty == "" {
			f.Errors = append(f.Errors, fmt.Sprintf("Duty party is required to generate ICP file, but is empty."))
			return
		}
		date, t := monthDt.Format(FileNameDateLayout), time.Now().Format(FileNameTimeLayout)
		f.FileName = fmt.Sprintf("%s_%s_%s.xlsx", f.DutyParty, date, t)
	} else {
		fp := strings.Split(f.FileName, "_")
		f.DutyParty = fp[0]
		dt := fp[1]
		d, err := time.Parse(FileNameDateLayout, dt)
		if err != nil {
			f.Errors = append(f.Errors, fmt.Sprintf("The ICP filename:%s invalid format(correct: BE0796544895_200601_02150405.xlsx)", f.FileName))
			return
		}
		monthDt = d
	}

	year, month := utils.GetCurrentYearMonth(monthDt)
	saveDir := filepath.Join(saveRoot, year, month)

	log.Println("ICP save dir: ", saveDir)
	if !utils.IsDir(saveDir) && !utils.CreateDir(saveDir) {
		f.Errors = append(f.Errors, fmt.Sprintf("Create save dir: %s failed.", saveDir))
		return
	}
	f.FilePath = filepath.Join(saveDir, f.FileName)
}

// createICPFile creates a ICP excel file
func (f *FileOfICP) createICPFile() {
	log.Println("**** Creating ICP excel ****")
	file := excelize.NewFile()
	icpDate := time.Now().Format(FileNameDateLayout)

	taxSheetName := fmt.Sprintf("%s_%s_%s", "ICP", f.DutyParty, icpDate)
	err := FillTaxSheet(file, taxSheetName, f.TaxData)
	if err != nil {
		f.Errors = append(f.Errors, fmt.Sprintf("Fill ICP sheet failed: %v", err))
	}

	taxFileSheetName := fmt.Sprintf("%s_%s_%s", "TAX", f.DutyParty, icpDate)
	err = FillTaxFileSheet(file, taxFileSheetName, f.TaxFileData)
	if err != nil {
		f.Errors = append(f.Errors, fmt.Sprintf("Fill ICP sheet failed: %v", err))
	}

	podSheetName := fmt.Sprintf("%s_%s_%s", "POD", f.DutyParty, icpDate)
	err = FillPodSheet(file, podSheetName, f.PodFileData)
	if err != nil {
		f.Errors = append(f.Errors, fmt.Sprintf("Fill POD sheet failed: %v", err))
	}

	log.Printf("**** Save ICP excel: %s ****\n", f.FilePath)
	if err := file.SaveAs(f.FilePath); err != nil {
		f.Errors = append(f.Errors, fmt.Sprintf("Save ICP file on disk failed: %v", err))
	}
}
