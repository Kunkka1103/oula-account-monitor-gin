package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

var (
	dbDsn     string
	zkrushDsn string
)

// 初始化命令行参数
func init() {
	// 定义命令行参数
	flag.StringVar(&dbDsn, "dbDsn", "", "PostgreSQL DSN")
	flag.StringVar(&zkrushDsn, "zkrushDsn", "", "ZkRush DSN")
	flag.Parse()
}

// 主要运行函数
func main() {
	// 检查 DSN 是否已传递
	if dbDsn == "" || zkrushDsn == "" {
		log.Fatal("Both dbDsn and zkrushDsn must be provided")
	}

	// 初始化 Gin 引擎
	r := gin.Default()

	// 加载前端页面
	r.LoadHTMLFiles("index.html")

	// 显示前端页面
	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", nil)
	})

	// 定义 API：处理主账户查询
	r.POST("/main-account", func(c *gin.Context) {
		mainAccountName := c.PostForm("mainAccountName")
		showDetails := c.PostForm("showDetails") == "true"

		// 连接数据库
		db, zkRushDB := connectDatabases(dbDsn, zkrushDsn)
		defer db.Close()
		defer zkRushDB.Close()

		if mainAccountName != "" {
			// 执行主账户查询
			result := runForMainAccount(db, zkRushDB, mainAccountName, showDetails)
			c.JSON(http.StatusOK, gin.H{
				"message": fmt.Sprintf("Main account %s processed", mainAccountName),
				"result":  result,
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid main account"})
		}
	})

	// 定义 API：处理子账户查询
	r.POST("/sub-account", func(c *gin.Context) {
		subAccountName := c.PostForm("subAccountName")
		showDetails := c.PostForm("showDetails") == "true"

		// 连接数据库
		db, zkRushDB := connectDatabases(dbDsn, zkrushDsn)
		defer db.Close()
		defer zkRushDB.Close()

		if subAccountName != "" {
			// 执行子账户查询
			result := runOnce(db, zkRushDB, subAccountName, showDetails)
			c.JSON(http.StatusOK, gin.H{
				"message": fmt.Sprintf("Sub-account %s processed", subAccountName),
				"result":  result,
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sub-account"})
		}
	})

	// 启动 Gin 服务，监听端口
	r.Run(":8080")
}

// 连接数据库的函数
func connectDatabases(dsn, zkrushDsn string) (*sql.DB, *sql.DB) {
	// 连接到 PostgreSQL 主库
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("连接主库数据库出错: %v", err)
	}

	// 连接到 zkRush 数据库
	zkRushDB, err := sql.Open("postgres", zkrushDsn)
	if err != nil {
		log.Fatalf("连接 zkrush 数据库出错: %v", err)
	}

	return db, zkRushDB
}

// 模拟的 runForMainAccount 和 runOnce 函数，替换为你自己的逻辑
func runForMainAccount(db *sql.DB, zkRushDB *sql.DB, mainAccountName string, showDetails bool) string {

	rows, err := db.Query(`
		SELECT ma.name AS sub_account_name
		FROM miner_account ma
		JOIN "user" u ON ma.main_user_id = u.id
		WHERE u.email = $1`, mainAccountName)
	if err != nil {
		log.Fatalf("Error fetching sub-accounts for main account %s: %v", mainAccountName, err)
	}
	defer rows.Close()

	// 遍历每个子账户并使用 runOnce 处理
	var res string
	for rows.Next() {
		var subAccountName string

		// 获取子账户名称
		err := rows.Scan(&subAccountName)
		if err != nil {
			log.Fatalf("Error scanning sub-account data: %v", err)
		}

		// 调用 runOnce 处理每个子账户
		res += runOnce(db, zkRushDB, subAccountName, showDetails)
	}
	return res
}

func getOverview(db *sql.DB, subAccountName string) (Overview, error) {
	var overview Overview

	// 调试输出
	//fmt.Printf("DEBUG: Fetching overview for sub-account: %s\n", subAccountName)

	SQL := fmt.Sprintf(`SELECT u.email AS main_account_name,
		COUNT(m.id) AS total_machines,
		COALESCE(SUM(CASE WHEN m.last_commit_solution >= EXTRACT(EPOCH FROM NOW()) - 600 THEN 1 ELSE 0 END), 0) AS active_machines,
		COALESCE(SUM(CASE WHEN m.last_commit_solution < EXTRACT(EPOCH FROM NOW()) - 600
	AND m.last_commit_solution >= EXTRACT(EPOCH FROM NOW()) - 86400 THEN 1 ELSE 0 END), 0) AS inactive_machines,
		COALESCE(SUM(CASE WHEN m.last_commit_solution < EXTRACT(EPOCH FROM NOW()) - 86400 THEN 1 ELSE 0 END), 0) AS failed_machines,
		COALESCE(SUM(CASE WHEN m.last_commit_solution IS NULL THEN 1 ELSE 0 END), 0) AS invalid_machines
	FROM miner_account ma
	LEFT JOIN machine m ON m.miner_account_id = ma.id
	JOIN "user" u ON ma.main_user_id = u.id
	WHERE ma.name = '%s'
	GROUP BY u.email`, subAccountName)

	// 查询概览信息
	err := db.QueryRow(SQL).Scan(&overview.MainAccountName, &overview.TotalMachines, &overview.ActiveMachines, &overview.InactiveMachines, &overview.FailedMachines, &overview.InvalidMachines)

	if err == sql.ErrNoRows {
		// 如果没有机器，将所有机器数量设置为 0，并返回概览
		fmt.Printf("子账户 %s 没有机器数据，所有机器数设为 0\n", subAccountName)
		overview.TotalMachines = 0
		overview.ActiveMachines = 0
		overview.InactiveMachines = 0
		overview.FailedMachines = 0
		overview.InvalidMachines = 0
		return overview, nil
	}

	if err != nil {
		// 打印错误日志
		fmt.Printf("DEBUG: Error fetching overview for sub-account: %s, SQL: %s, error: %v\n", subAccountName, SQL, err)
		return overview, err
	}

	// 当没有机器时，将无效机器数设为 0
	if overview.TotalMachines == 0 {
		overview.InvalidMachines = 0
	}

	return overview, nil
}

// Machine holds the details for each machine
type Machine struct {
	CreatedAt          string
	Name               string
	Project            string
	LastCommitSolution string
	Status             string
}

// Overview holds the overview information for the sub-account
type Overview struct {
	MainAccountName  string
	TotalMachines    int
	ActiveMachines   int
	InactiveMachines int
	FailedMachines   int
	InvalidMachines  int
	Machines         []Machine
}

func runOnce(db *sql.DB, zkRushDB *sql.DB, subAccountName string, showDetails bool) string {
	// 获取当前时间
	now := time.Now()

	// 概览信息
	overview, err := getOverview(db, subAccountName)
	if err != nil {
		log.Printf("Error fetching overview for %s: %v", subAccountName, err)
		return fmt.Sprintf("Error fetching overview for %s: %v", subAccountName, err)
	}

	var mainAccountID, subAccountID int
	err = db.QueryRow(`
		SELECT u.id AS main_account_id, ma.id AS sub_account_id, u.email AS main_account_name
		FROM miner_account ma
		JOIN "user" u ON ma.main_user_id = u.id
		WHERE ma.name = $1`, subAccountName).Scan(&mainAccountID, &subAccountID, &overview.MainAccountName)
	if err != nil {
		log.Fatalf("Error fetching overview for %s: %v", subAccountName, err)
		return fmt.Sprintf("Error fetching overview for %s: %v", subAccountName, err)
	}

	// 获取收益明细
	rewardRecord := showSubAccountRewards(db, subAccountName)

	// 显示概览信息（添加中英文标注）
	res := fmt.Sprintf("<h2>子账户总览信息 - Sub-account Overview for %s(%d)</h2>", subAccountName, subAccountID) +
		fmt.Sprintf("<p>主账户名称 - Main Account: %s(%d)</p>", overview.MainAccountName, mainAccountID) +
		fmt.Sprintf("<p>总机器数 - Total Machines: %d</p>", overview.TotalMachines) +
		fmt.Sprintf("<p>活跃机器数 - Active Machines: %d</p>", overview.ActiveMachines) +
		fmt.Sprintf("<p>不活跃机器数 - Inactive Machines: %d</p>", overview.InactiveMachines) +
		fmt.Sprintf("<p>失效机器数 - Failed Machines: %d</p>", overview.FailedMachines) +
		fmt.Sprintf("<p>无效机器数 - Invalid Machines: %d</p>", overview.InvalidMachines) +
		fmt.Sprintf("%s", rewardRecord)

	// 如果显示详情
	if showDetails {
		// 获取每台机器的详细信息
		rows, err := db.Query(`
			SELECT m.created_at, m.name, m.project, m.last_commit_solution
			FROM machine m
			JOIN miner_account ma ON m.miner_account_id = ma.id
			WHERE ma.name = $1`, subAccountName)
		if err != nil {
			log.Fatalf("Error fetching machine details for %s: %v", subAccountName, err)
		}
		defer rows.Close()

		// 构建HTML表格
		res += `<h3>机器详细信息 - Machine Details</h3>`
		res += `<table border="1" cellpadding="5" cellspacing="0">`
		res += `<tr><th>子账户</th><th>创建时间</th><th>机器名称</th><th>项目</th><th>最近提交时间</th><th>时间差(秒)</th><th>状态</th></tr>`

		// 遍历机器记录
		for rows.Next() {
			var machine Machine
			var lastCommitTimestamp sql.NullInt64

			// 获取机器的创建时间、名称、项目和最后提交算力时间戳（允许为 null）
			err := rows.Scan(&machine.CreatedAt, &machine.Name, &machine.Project, &lastCommitTimestamp)
			if err != nil {
				log.Printf("Error scanning machine details for %s: %v", subAccountName, err)
				continue
			}

			// 转换 last_commit_solution 为上海时间并计算时间差，如果为 null，状态为 Invalid
			lastCommitTime, timeDiff, status := ConvertToShanghaiTime(lastCommitTimestamp, now)
			machine.Status = status

			// 构建表格内容
			if lastCommitTimestamp.Valid {
				res += fmt.Sprintf(
					`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%.0f</td><td>%s</td></tr>`,
					subAccountName, machine.CreatedAt, machine.Name, machine.Project,
					lastCommitTime.Format("2006-01-02 15:04:05"), timeDiff.Seconds(), machine.Status,
				)
			} else {
				res += fmt.Sprintf(
					`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>无效</td><td>-</td><td>%s</td></tr>`,
					subAccountName, machine.CreatedAt, machine.Name, machine.Project, machine.Status,
				)
			}
		}
		res += `</table>` // 结束表格
	}
	return res
}

func ConvertToShanghaiTime(timestamp sql.NullInt64, now time.Time) (time.Time, time.Duration, string) {
	if !timestamp.Valid {
		// 如果 last_commit_solution 是 null，返回无效状态
		return time.Time{}, 0, "Invalid (无效)"
	}

	// 将时间戳转换为时间并设置为上海时区
	location, _ := time.LoadLocation("Asia/Shanghai")
	lastCommitTime := time.Unix(timestamp.Int64, 0).In(location)
	timeDifference := now.Sub(lastCommitTime) // 计算时间差

	// 根据时间差返回状态
	if timeDifference.Seconds() <= ActiveThreshold {
		return lastCommitTime, timeDifference, "Active (活跃)"
	} else if timeDifference.Seconds() <= InactiveThreshold {
		return lastCommitTime, timeDifference, "Inactive (不活跃)"
	} else {
		return lastCommitTime, timeDifference, "Failed (失效)"
	}
}

// Time difference thresholds (in seconds)
const (
	ActiveThreshold   = 600   // 10 minutes
	InactiveThreshold = 86400 // 24 hours
)

func showSubAccountRewards(db *sql.DB, subAccountName string) string {
	var rows *sql.Rows
	var err error
	var res string

	// 查询收益明细，转换 created_at 为北京时间
	rows, err = db.Query(`
		SELECT
    created_at AT TIME ZONE 'Asia/Shanghai' AS created_at,
    reward,
    pay_status
FROM
    distributor
WHERE
    miner_account_id = (SELECT id FROM miner_account WHERE name = $1)
ORDER BY
    created_at DESC;`, subAccountName)
	if err != nil {
		log.Printf("Error fetching reward records for sub-account %s: %v", subAccountName, err)
		return ""
	}
	defer rows.Close()

	var totalReward float64
	err = db.QueryRow(`
		SELECT COALESCE(SUM(reward), 0) as total_reward 
		FROM distributor 
		WHERE miner_account_id = (SELECT id FROM miner_account WHERE name = $1) 
		AND status = 'verified'`, subAccountName).Scan(&totalReward)
	if err != nil {
		log.Printf("Error fetching total reward for sub-account %s: %v", subAccountName, err)
		return res
	}

	// 显示总收益

	// 构建HTML表格
	res += `<h3>收益明细 - Reward Records</h3>`
	res += fmt.Sprintf("<p><strong>总收益 (已验证) - Total Verified Reward: %.2f</strong></p>", totalReward)

	res += `<table border="1" cellpadding="5" cellspacing="0">`
	res += `<tr><th>日期 (Created At)</th><th>奖励 (Reward)</th><th>支付状态 (Pay Status)</th></tr>`

	// 遍历查询结果并添加到表格
	for rows.Next() {
		var createdAt time.Time
		var reward float64
		var payStatus string

		err := rows.Scan(&createdAt, &reward, &payStatus)
		if err != nil {
			log.Printf("Error scanning reward records: %v", err)
			continue
		}

		// 将每一行记录添加到表格
		res += fmt.Sprintf("<tr><td>%s</td><td>%.2f</td><td>%s</td></tr>", createdAt.Format("2006-01-02 15:04:05"), reward, payStatus)
	}
	res += `</table>` // 结束表格

	// 查询总收益（已验证的收益）

	return res
}
