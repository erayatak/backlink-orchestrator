package main
import (
	"html/template"
	"os"
	"strconv"
)
func main() {
	tmpl := template.Must(template.New("jobs.html").Funcs(template.FuncMap{
		"percentage": func(val, total int) float64 {
			if total == 0 {
				return 0
			}
			return float64(val) / float64(total) * 100
		},
		"formatNumber": func(n int) string {
			return strconv.Itoa(n)
		},
	}).ParseFiles("d:/Orchestrator/backlink-orchestrator/web/templates/jobs.html"))
	
	type JobRow struct {
		JobID          string
		Dataset        string
		CrawlID        string
		Status         string
		TotalTasks     int
		SucceededTasks int
		FailedTasks    int
	}
	jobs := []JobRow{
		{"123", "CC", "CRAWL", "RUNNING", 100, 50, 0},
	}
	err := tmpl.ExecuteTemplate(os.Stdout, "jobs.html", map[string]interface{}{"Jobs": jobs})
	if err != nil {
		panic(err)
	}
}
