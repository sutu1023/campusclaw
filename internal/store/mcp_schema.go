package store

// MCP history is deliberately not cascaded from conversations/messages or
// server deletion. Servers are tombstoned; historical IDs remain evidence even
// when an owner removes the original conversation. Only current tool rows are
// executable; discovery snapshots are immutable comparison data.
var mcpSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS mcp_servers (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		class_id BIGINT NOT NULL,
		name VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		base_url VARCHAR(2048) NOT NULL,
		credential_ref CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		state ENUM('disconnected','connected','pending_approval') NOT NULL DEFAULT 'disconnected',
		generation BIGINT UNSIGNED NOT NULL DEFAULT 0,
		approved_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
		pending_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
		last_error VARCHAR(500) NOT NULL DEFAULT '',
		deleted_at DATETIME(6) NULL,
		created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
		UNIQUE KEY uk_mcp_server_name (class_id, name),
		INDEX idx_mcp_servers_class (class_id, deleted_at),
		CONSTRAINT fk_mcp_servers_class FOREIGN KEY (class_id) REFERENCES classes(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS mcp_tools (
		server_id BIGINT NOT NULL,
		name VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		description TEXT NOT NULL,
		input_schema MEDIUMTEXT NOT NULL,
		snapshot_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		PRIMARY KEY (server_id, name),
		CONSTRAINT fk_mcp_tools_server FOREIGN KEY (server_id) REFERENCES mcp_servers(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS mcp_discovery_snapshots (
		server_id BIGINT NOT NULL,
		snapshot_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		tools_json MEDIUMTEXT NOT NULL,
		created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		PRIMARY KEY (server_id, snapshot_hash),
		CONSTRAINT fk_mcp_snapshots_server FOREIGN KEY (server_id) REFERENCES mcp_servers(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS mcp_discovery_changes (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		class_id BIGINT NOT NULL,
		server_id BIGINT NOT NULL,
		previous_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		snapshot_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		changed_tools_json TEXT NOT NULL,
		created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		approved_by BIGINT NULL,
		approved_at DATETIME(6) NULL,
		INDEX idx_mcp_changes_class (class_id, created_at, id),
		INDEX idx_mcp_changes_server (server_id, id),
		CONSTRAINT fk_mcp_changes_server FOREIGN KEY (server_id) REFERENCES mcp_servers(id),
		CONSTRAINT fk_mcp_changes_class FOREIGN KEY (class_id) REFERENCES classes(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS mcp_tool_calls (
		id BIGINT PRIMARY KEY AUTO_INCREMENT,
		class_id BIGINT NOT NULL,
		server_id BIGINT NOT NULL,
		server_name VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		tool_ref VARCHAR(97) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		user_id BIGINT NOT NULL,
		conversation_id BIGINT NOT NULL,
		question_message_id BIGINT NOT NULL,
		call_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		round INT NOT NULL,
		status ENUM('ok','error','denied') NOT NULL DEFAULT 'error',
		safe_text VARCHAR(500) NOT NULL,
		duration_ms BIGINT UNSIGNED NOT NULL DEFAULT 0,
		created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
		completed_at DATETIME(6) NULL,
		UNIQUE KEY uk_mcp_call_id (class_id, call_id),
		INDEX idx_mcp_calls_class_time (class_id, created_at, id),
		INDEX idx_mcp_calls_question (class_id, conversation_id, question_message_id),
		INDEX idx_mcp_calls_filter (class_id, server_id, status, id),
		CONSTRAINT fk_mcp_calls_server FOREIGN KEY (server_id) REFERENCES mcp_servers(id),
		CONSTRAINT fk_mcp_calls_class FOREIGN KEY (class_id) REFERENCES classes(id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

	`CREATE TABLE IF NOT EXISTS mcp_conversation_usage (
		conversation_id BIGINT PRIMARY KEY,
		reserved_count INT UNSIGNED NOT NULL DEFAULT 0,
		CONSTRAINT fk_mcp_usage_conversation FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
}
