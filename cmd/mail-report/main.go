package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/appscode-images/builder/api"
	"github.com/appscode-images/builder/lib"
	"github.com/olekukonko/tablewriter"
	flag "github.com/spf13/pflag"
	"gomodules.xyz/mailer"
)

func main() {
	var name = flag.String("name", "", "Name of binary")
	flag.Parse()

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	reports, err := GatherReport(dir, *name)
	if err != nil {
		panic(err)
	}
	data := GenerateMarkdownReport(*name, reports)

	readmeFile := filepath.Join(dir, "library", *name, "README.md")
	err = os.WriteFile(readmeFile, data, 0644)
	if err != nil {
		panic(err)
	}

	smtp, err := mailer.NewSMTPServiceFromEnv()
	if err != nil {
		panic(err)
	}
	m := mailer.Mailer{
		Sender:          "tamal@appscode.com",
		BCC:             "",
		ReplyTo:         "",
		Subject:         "CVE report: " + *name,
		Body:            string(data),
		Plaintext:       true,
		Params:          nil,
		AttachmentBytes: nil,
		GDriveFiles:     nil,
		GoogleDocIds:    nil,
	}
	err = m.SendMail(smtp, "tamal@appscode.com", "", nil)
	if err != nil {
		panic(err)
	}

	autoPromoteTags := make([]string, 0, len(reports))
	for _, report := range reports {
		if report.AutoPromote() {
			autoPromoteTags = append(autoPromoteTags, report.Tag)
		}
	}
	if len(autoPromoteTags) > 0 {
		filename := filepath.Join(dir, "library", *name, "promote_tags.txt")
		err = os.WriteFile(filename, []byte(strings.Join(autoPromoteTags, "\n")), 0644)
		if err != nil {
			panic(err)
		}
	}
}

type TagReport struct {
	Tag      string
	Ref      string
	Critical Stats
	High     Stats
	Medium   Stats
	Low      Stats
	Unknown  Stats
}

type Stats struct {
	Before int
	After  int
}

func (s Stats) String() string {
	b, a := "?", "?"
	if s.Before >= 0 {
		b = strconv.Itoa(s.Before)
	}
	if s.After >= 0 {
		a = strconv.Itoa(s.After)
	}
	return fmt.Sprintf("%s -> %s", b, a)
}

func (r TagReport) AutoPromote() bool {
	if (r.Critical.Before + r.High.Before + r.Medium.Before) < 0 {
		return true // no previous image
	}
	return (r.Critical.Before+r.High.Before+r.Medium.Before) > 0 &&
		r.Critical.Before >= 0 && r.Critical.After == 0 &&
		r.High.Before >= 0 && r.High.After == 0 &&
		r.Medium.Before >= 0 && r.Medium.After == 0
}

func (r TagReport) NoPriorCVE() bool {
	return (r.Critical.Before +
		r.High.Before +
		r.Medium.Before +
		r.Low.Before +
		r.Unknown.Before) == 0
}

func (r TagReport) Headers() []string {
	return []string{
		"Tag",
		"Ref",
		"Promote",
		"Critical",
		"High",
		"Medium",
		"Low",
		"Unknown",
	}
}

func (r TagReport) Strings() []string {
	promote := "?"
	if r.AutoPromote() {
		promote = "Y"
	} else if r.NoPriorCVE() {
		promote = "N"
	}

	return []string{
		r.Tag,
		r.Ref,
		promote,
		r.Critical.String(),
		r.High.String(),
		r.Medium.String(),
		r.Low.String(),
		r.Unknown.String(),
	}
}

func GatherReport(dir, name string) ([]TagReport, error) {
	tags, err := lib.ListAppTags(dir, name)
	if err != nil {
		return nil, err
	}

	t := time.Now()
	ts := t.UTC().Format("20060102")
	sh := lib.NewShell()

	reports := make([]TagReport, 0, len(tags))
	for _, tag := range tags {
		tagReport := TagReport{
			Tag: tag,
			Ref: fmt.Sprintf("%s_%s", tag, ts),
		}

		ref := fmt.Sprintf("%s/%s:%s", api.DOCKER_REGISTRY, name, tag)
		if found, err := lib.ImageExists(ref); err != nil {
			return nil, err
		} else if found {
			report, err := lib.Scan(sh, ref)
			if err != nil {
				return nil, err
			}
			for sev, count := range lib.SummarizeReport(report) {
				switch sev {
				case "CRITICAL":
					tagReport.Critical.Before = count
				case "HIGH":
					tagReport.High.Before = count
				case "MEDIUM":
					tagReport.Medium.Before = count
				case "LOW":
					tagReport.Low.Before = count
				case "UNKNOWN":
					tagReport.Unknown.Before = count
				}
			}
		} else {
			tagReport.Critical.Before = -1
			tagReport.High.Before = -1
			tagReport.Medium.Before = -1
			tagReport.Low.Before = -1
			tagReport.Unknown.Before = -1
		}

		tsRef := fmt.Sprintf("%s/%s:%s_%s", api.DOCKER_REGISTRY, name, tag, ts)
		if found, err := lib.ImageExists(tsRef); err != nil {
			return nil, err
		} else if found {
			tsReport, err := lib.Scan(sh, tsRef)
			if err != nil {
				return nil, err
			}
			for sev, count := range lib.SummarizeReport(tsReport) {
				switch sev {
				case "CRITICAL":
					tagReport.Critical.After = count
				case "HIGH":
					tagReport.High.After = count
				case "MEDIUM":
					tagReport.Medium.After = count
				case "LOW":
					tagReport.Low.After = count
				case "UNKNOWN":
					tagReport.Unknown.After = count
				}
			}
		} else {
			tagReport.Critical.After = -1
			tagReport.High.After = -1
			tagReport.Medium.After = -1
			tagReport.Low.After = -1
			tagReport.Unknown.After = -1
		}

		reports = append(reports, tagReport)
	}

	return reports, nil
}

func GenerateMarkdownReport(name string, reports []TagReport) []byte {
	var buf bytes.Buffer
	buf.WriteString("# CVE Report:" + name)
	buf.WriteRune('\n')
	buf.Write(generateMarkdownTable(reports))

	return buf.Bytes()
}

func generateMarkdownTable(reports []TagReport) []byte {
	var tr TagReport

	data := make([][]string, 0, len(reports))
	for _, r := range reports {
		data = append(data, r.Strings())
	}
	sort.Slice(data, func(i, j int) bool {
		return data[i][0] < data[j][0]
	})

	var buf bytes.Buffer

	table := tablewriter.NewWriter(&buf)
	table.SetHeader(tr.Headers())
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.AppendBulk(data) // Add Bulk Data
	table.Render()

	return buf.Bytes()
}
