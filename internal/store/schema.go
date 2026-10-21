package store

// schemaStatements is the ordered DDL for the MySQL schema.
//
// Every material / knowledge table carries a non-null, indexed class_id tenant
// column. knowledge_chunks is the search source of truth and carries the ngram
// full-text index used by the keyword retrieval path.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS classes (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		name VARCHAR(255) NOT NULL UNIQUE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS users (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		username VARCHAR(255) NOT NULL UNIQUE,
		display_name VARCHAR(255) NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		role ENUM('teacher','student') NOT NULL,
		class_id BIGINT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_users_class (class_id),
		CONSTRAINT fk_users_class FOREIGN KEY (class_id) REFERENCES classes(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS sessions (
		token_hash CHAR(64) PRIMARY KEY,
		user_id BIGINT NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_sessions_user (user_id),
		INDEX idx_sessions_expires (expires_at),
		CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS materials (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		class_id BIGINT NOT NULL,
		title VARCHAR(512) NOT NULL,
		file_path VARCHAR(1024) NULL,
		uploaded_by BIGINT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_materials_class (class_id),
		CONSTRAINT fk_materials_class FOREIGN KEY (class_id) REFERENCES classes(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS knowledge_entries (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		material_id BIGINT NOT NULL,
		class_id BIGINT NOT NULL,
		body_text MEDIUMTEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_ke_class (class_id),
		INDEX idx_ke_material (material_id),
		CONSTRAINT fk_ke_material FOREIGN KEY (material_id) REFERENCES materials(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS knowledge_chunks (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		knowledge_entry_id BIGINT NOT NULL,
		class_id BIGINT NOT NULL,
		chunk_index INT NOT NULL,
		start_offset INT NOT NULL,
		end_offset INT NOT NULL,
		chunk_text MEDIUMTEXT NOT NULL,
		index_status ENUM('pending','ready','failed') NOT NULL DEFAULT 'pending',
		embedding_model VARCHAR(255) NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_kc_class (class_id),
		INDEX idx_kc_entry (knowledge_entry_id),
		INDEX idx_kc_status (index_status),
		FULLTEXT KEY ft_chunk_text (chunk_text) WITH PARSER ngram
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	// tutor_agents is the per-class configuration unit: a role prompt plus
	// bindings (in the two join tables below) to the skills it may route to and
	// the materials it may retrieve from.
	`CREATE TABLE IF NOT EXISTS tutor_agents (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		class_id BIGINT NOT NULL,
		name VARCHAR(64) NOT NULL,
		prompt VARCHAR(2000) NOT NULL DEFAULT '',
		enabled TINYINT(1) NOT NULL DEFAULT 1,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		UNIQUE KEY uk_agents_class_name (class_id, name),
		INDEX idx_agents_class (class_id),
		CONSTRAINT fk_agents_class FOREIGN KEY (class_id) REFERENCES classes(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS skills (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		class_id BIGINT NOT NULL,
		name VARCHAR(32) NOT NULL,
		trigger_text VARCHAR(120) NOT NULL,
		description VARCHAR(2000) NOT NULL,
		tools_json VARCHAR(255) NOT NULL DEFAULT '',
		source_text MEDIUMTEXT NOT NULL,
		subject_text VARCHAR(32) NOT NULL DEFAULT '',
		enabled TINYINT(1) NOT NULL DEFAULT 1,
		seed_key VARCHAR(64) NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		UNIQUE KEY uk_skills_class_name (class_id, name),
		UNIQUE KEY uk_skills_seed (seed_key),
		INDEX idx_skills_class (class_id),
		CONSTRAINT fk_skills_class FOREIGN KEY (class_id) REFERENCES classes(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS tutor_agent_skills (
		agent_id BIGINT NOT NULL,
		skill_id BIGINT NOT NULL,
		PRIMARY KEY (agent_id, skill_id),
		INDEX idx_tas_skill (skill_id),
		CONSTRAINT fk_tas_agent FOREIGN KEY (agent_id) REFERENCES tutor_agents(id) ON DELETE CASCADE,
		CONSTRAINT fk_tas_skill FOREIGN KEY (skill_id) REFERENCES skills(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS tutor_agent_materials (
		agent_id BIGINT NOT NULL,
		material_id BIGINT NOT NULL,
		PRIMARY KEY (agent_id, material_id),
		INDEX idx_tam_material (material_id),
		CONSTRAINT fk_tam_agent FOREIGN KEY (agent_id) REFERENCES tutor_agents(id) ON DELETE CASCADE,
		CONSTRAINT fk_tam_material FOREIGN KEY (material_id) REFERENCES materials(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	// A conversation locks one agent at creation time; class_id and user_id are
	// the two ownership filters that must both match on every read.
	`CREATE TABLE IF NOT EXISTS conversations (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		class_id BIGINT NOT NULL,
		user_id BIGINT NOT NULL,
		agent_id BIGINT NOT NULL,
		title VARCHAR(255) NOT NULL,
		created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
		INDEX idx_conv_user (user_id),
		INDEX idx_conv_class (class_id),
		INDEX idx_conv_agent (agent_id),
		CONSTRAINT fk_conv_class FOREIGN KEY (class_id) REFERENCES classes(id),
		CONSTRAINT fk_conv_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
		CONSTRAINT fk_conv_agent FOREIGN KEY (agent_id) REFERENCES tutor_agents(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS messages (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		conversation_id BIGINT NOT NULL,
		role ENUM('user','assistant') NOT NULL,
		content MEDIUMTEXT NOT NULL,
		citations_json MEDIUMTEXT NULL,
		skill_name VARCHAR(64) NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_msg_conv (conversation_id),
		CONSTRAINT fk_msg_conv FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
}
