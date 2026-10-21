// Package store owns all MySQL persistence for CampusClaw.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

// Store wraps a MySQL connection pool.
type Store struct {
	DB *sql.DB
}

// Open connects to MySQL and waits until it is reachable.
func Open(ctx context.Context, dsn string, wait time.Duration) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	deadline := time.Now().Add(wait)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = db.PingContext(pingCtx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = db.Close()
			return nil, fmt.Errorf("mysql not reachable within %s: %w", wait, err)
		}
		select {
		case <-ctx.Done():
			_ = db.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return &Store{DB: db}, nil
}

// Close releases the connection pool.
func (s *Store) Close() error { return s.DB.Close() }

// Migrate creates the schema (idempotent).
func (s *Store) Migrate(ctx context.Context) error {
	for _, stmt := range schemaStatements {
		if _, err := s.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("schema statement failed: %w", err)
		}
	}
	return nil
}

// Seed inserts the demo classes, users, materials and one searchable entry per
// class. It is idempotent and safe to run on every start.
func (s *Store) Seed(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO classes (id, name) VALUES (1,'班级 A'),(2,'班级 B')
		 ON DUPLICATE KEY UPDATE name=VALUES(name)`); err != nil {
		return fmt.Errorf("seed classes: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("CampusClaw#2026"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash seed password: %w", err)
	}
	users := []struct {
		username, display, role string
		classID                 int64
	}{
		{"teacher_a", "教师 A", "teacher", 1},
		{"student_a1", "学生 A1", "student", 1},
		{"student_b1", "学生 B1", "student", 2},
	}
	for _, u := range users {
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO users (username, display_name, password_hash, role, class_id)
			 VALUES (?,?,?,?,?)
			 ON DUPLICATE KEY UPDATE display_name=VALUES(display_name),
			   password_hash=VALUES(password_hash), role=VALUES(role), class_id=VALUES(class_id)`,
			u.username, u.display, string(hash), u.role, u.classID); err != nil {
			return fmt.Errorf("seed user %s: %w", u.username, err)
		}
	}

	seeds := []struct {
		classID int64
		title   string
		body    string
	}{
		{1, "A 班-课堂提问技巧.md", `# A 班课堂提问技巧

一、提问的目的
课堂提问不是简单检查记忆，而是推动学生思考。好的问题能把学生的注意力引向关键概念，也能暴露理解中的偏差，帮助教师及时调整讲解节奏。

二、问题的层次
可以把问题分为记忆、理解、应用、分析四类。记忆类问题只需复述事实，理解类问题要求解释原因，应用类问题要求把方法用到新情境，分析类问题要求比较与判断。教师应根据教学目标选择层次，避免整节课都停留在记忆层面。

三、等待时间
提问后留出三到五秒的等待时间，往往能显著提高回答质量。等待时间过短，学生来不及组织语言；适当延长后，更多学生愿意举手，回答也更完整。

四、追问与转问
当学生回答不完整时，可以用追问帮助其补充理由；当无人举手时，可以把问题转给小组讨论后再回答。追问要围绕同一个关键点，避免不断更换主题。

五、常见误区
只叫举手的学生、只问“对不对”、把提问当作惩罚，都会削弱提问的价值。教师应尽量覆盖不同水平的学生，并给出具体的反馈。

本班教研专属词：青竹，用于本班资料的区分与隔离验收。`},
		{1, "A 班-分数教学教研笔记.md", `# A 班分数教学教研笔记

一、分数的意义
把一个整体平均分成若干份，表示这样一份或几份的数，叫作分数。分母表示平均分成的份数，分子表示取了其中的几份。教学中要强调“平均分”这一前提。

二、分数单位
把单位“1”平均分成若干份，表示其中一份的数叫作分数单位。理解分数单位，有助于比较分数大小和进行加减运算。

三、通分与约分
通分是把异分母分数化成同分母分数，约分是把分数化成最简分数。两者都依据分数的基本性质：分子和分母同时乘或除以相同的数（零除外），分数的大小不变。

四、常见错误
学生常把“分成几份”误当作“平均分成几份”，或在比较分数时只看分子大小。教学时应借助图形与实物，帮助学生建立直观。

五、教研改进
本组计划在下一次教研中，围绕“分数单位”设计一组变式练习，并观察学生是否能说明理由。

本班教研专属词：青竹，用于本班资料的区分与隔离验收。`},
		{1, "A 班-勾股定理教案.md", `# A 班勾股定理教案

一、教学目标
理解勾股定理的含义，掌握直角三角形两条直角边与斜边的关系，能运用定理解决简单的实际问题。

二、情境导入
出示直角边分别为 3 与 4 的直角三角形，先估计斜边，再用方格纸测量验证，学生容易发现斜边约为 5，从而产生疑问。

三、探究新知
在三条边上分别作正方形，通过数格子发现两个较小正方形的面积之和等于最大正方形的面积，即两条直角边的平方和等于斜边的平方。

四、例题与练习
已知直角边为 6 与 8，求斜边，得 10。再练习已知斜边与一条直角边求另一条直角边，提醒先判断哪条边是斜边。

五、课堂小结
勾股定理把直角三角形的形状与数量联系起来，是后续学习三角函数与几何证明的重要基础。

本班教研专属词：青竹，用于本班资料的区分与隔离验收。`},
		{2, "B 班-课堂提问技巧.md", `# B 班课堂提问技巧

一、提问的价值
提问能激活已有经验，也能让教师了解学生的真实理解。设计问题时，要指向本节课的核心概念，而不是零散的事实。

二、由浅入深的层次
从记忆性问题开始，逐步过渡到理解与应用。比如先问“圆周长与什么有关”，再问“为什么周长与直径的比值是常数”，最后问“怎样用这个关系解决问题”。

三、等待与倾听
提出问题后耐心等待，认真倾听学生的表达，不急于替学生说完。倾听能发现思维过程，也为追问提供依据。

四、面向全体
避免只叫少数积极的学生。可以采用先独立思考、再同伴交流、最后全班分享的方式，让更多学生参与。

五、反馈
对回答给出具体反馈，指出好在哪里、还可以怎样改进，而不是简单地说“很好”。

本班教研专属词：红叶，用于本班资料的区分与隔离验收。`},
		{2, "B 班-分数教学教研笔记.md", `# B 班分数教学教研笔记

一、分数的产生
在测量与分物中，往往不能用整数表示结果，于是产生了分数。教学中应从真实情境引入，让学生体会分数的必要性。

二、分数的意义
把一个整体平均分成若干份，表示这样一份或几份的数叫作分数。要反复强调平均分，并借助图形帮助学生理解。

三、分数与除法
分数与除法有密切联系：被除数相当于分子，除数相当于分母（除数不为零）。这一关系为后续学习比和百分数打基础。

四、比较与运算
比较分数时可以通分，也可以与中间量比较。运算时要先理解算理，再追求熟练，避免只记步骤。

五、教研改进
本组拟收集学生的典型错误，分析其成因，并在课堂中设计针对性的追问。

本班教研专属词：红叶，用于本班资料的区分与隔离验收。`},
		{2, "B 班-圆周率教案.md", `# B 班圆周率教案

一、教学目标
理解圆的周长与直径的关系，认识圆周率，能运用圆的周长公式解决简单问题。

二、情境导入
让学生用绳子测量几个大小不同的圆的周长与直径，记录数据并计算比值。

三、探究新知
测量结果都接近 3.14，说明圆的周长总是直径的圆周率倍。圆周率是一个固定的数，通常取 3.14 进行计算。

四、例题与练习
已知直径为 10 厘米，求周长，约为 31.4 厘米。再练习已知周长求直径。

五、课堂小结
圆周率把圆的周长与直径联系起来，是研究圆的重要基础。

本班教研专属词：红叶，用于本班资料的区分与隔离验收。`},
	}
	for _, sd := range seeds {
		if err := s.ensureMaterial(ctx, sd.classID, sd.title, sd.body); err != nil {
			return err
		}
	}
	if err := s.seedSkills(ctx); err != nil {
		return err
	}
	return nil
}

// seedSkills preloads the two "解题引导" skills for class A. seed_key makes the
// insert idempotent, so repeated boots never duplicate them.
func (s *Store) seedSkills(ctx context.Context) error {
	seeds := []struct {
		key, name, trigger, subject, source string
	}{
		{
			key:     "a-math-guide",
			name:    "数学解题引导",
			trigger: "方程,去括号,分数,通分,面积,应用题,未知数,行程",
			subject: "数学",
			source: "名称：数学解题引导\n适用条件：方程,去括号,分数,通分,面积,应用题,未知数,行程\n资料：数学\n工具：knowledge_search\n启用：是\n\n" +
				"学生正在自己做数学作业，往往已经写了一步。先看懂他卡在哪一步，只指出下一步该检查什么，例如去括号有没有乘到每一项、分母不同要不要先通分、该设哪个未知数。不要算出最终得数，不要把整道题做完。依据只用数学自学笔记，并标出材料标题和位置。",
		},
		{
			key:     "a-chinese-guide",
			name:    "语文解题引导",
			trigger: "春晓,默写,阅读,中心,作文,开头,古诗,词语",
			subject: "语文",
			source: "名称：语文解题引导\n适用条件：春晓,默写,阅读,中心,作文,开头,古诗,词语\n资料：语文\n工具：knowledge_search\n启用：是\n\n" +
				"学生正在自己背诵、做阅读或改作文，卡在某一句。先接住他已经写出的那一句，告诉他往前看哪一句、找哪类句子。不要把诗的下一句、中心句或改好的段落直接写出来。依据只用语文自学笔记，并标出材料标题和位置。",
		},
	}
	for _, sd := range seeds {
		// The body is everything after the first blank line; for the seed we keep
		// it identical to the source so the parsed description matches the file.
		_, body := splitSkillSeed(sd.source)
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO skills (class_id, name, trigger_text, description, tools_json, source_text, subject_text, enabled, seed_key)
			 VALUES (1,?,?,?,?,?,?,1,?)
			 ON DUPLICATE KEY UPDATE name=VALUES(name), trigger_text=VALUES(trigger_text),
			   description=VALUES(description), tools_json=VALUES(tools_json), source_text=VALUES(source_text),
			   subject_text=VALUES(subject_text), enabled=VALUES(enabled)`,
			sd.name, sd.trigger, body, `["knowledge_search"]`, sd.source, sd.subject, sd.key); err != nil {
			return fmt.Errorf("seed skill %s: %w", sd.name, err)
		}
	}
	return nil
}

// splitSkillSeed returns the metadata and body halves of a skill file at the
// first blank line.
func splitSkillSeed(source string) (string, string) {
	for _, sep := range []string{"\n\n", "\r\n\r\n"} {
		if idx := strings.Index(source, sep); idx >= 0 {
			return source[:idx], strings.TrimSpace(source[idx+len(sep):])
		}
	}
	return source, ""
}

func (s *Store) ensureMaterial(ctx context.Context, classID int64, title, body string) error {
	var materialID int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM materials WHERE class_id=? AND title=? LIMIT 1`, classID, title).Scan(&materialID)
	switch {
	case err == sql.ErrNoRows:
		res, err := s.DB.ExecContext(ctx,
			`INSERT INTO materials (class_id, title) VALUES (?,?)`, classID, title)
		if err != nil {
			return fmt.Errorf("seed material %s: %w", title, err)
		}
		materialID, err = res.LastInsertId()
		if err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("lookup material %s: %w", title, err)
	default:
		// Material already present; ensure its entry exists too.
	}

	var entryID int64
	err = s.DB.QueryRowContext(ctx,
		`SELECT id FROM knowledge_entries WHERE material_id=? LIMIT 1`, materialID).Scan(&entryID)
	switch {
	case err == sql.ErrNoRows:
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO knowledge_entries (material_id, class_id, body_text) VALUES (?,?,?)`,
			materialID, classID, body); err != nil {
			return fmt.Errorf("seed entry for %s: %w", title, err)
		}
	case err != nil:
		return fmt.Errorf("lookup entry for %s: %w", title, err)
	}
	return nil
}
