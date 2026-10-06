-- MySQL 8.0 schema for the layered microwave planning service.
CREATE TABLE IF NOT EXISTS layer_heads (
  layer VARCHAR(20) PRIMARY KEY,
  version BIGINT NOT NULL,
  updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT INTO layer_heads(layer, version, updated_at)
VALUES ('nominal', 0, NOW(6)), ('batch', 0, NOW(6)), ('site', 0, NOW(6))
ON DUPLICATE KEY UPDATE layer = layer;

CREATE TABLE IF NOT EXISTS param_sets (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  layer VARCHAR(20) NOT NULL,
  scope_kind VARCHAR(40) NOT NULL,
  scope_id VARCHAR(64) NOT NULL,
  version BIGINT NOT NULL,
  fields JSON NOT NULL,
  published_at DATETIME(6) NOT NULL,
  published_by VARCHAR(64) NOT NULL DEFAULT '',
  comment VARCHAR(500) NOT NULL DEFAULT '',
  UNIQUE KEY uq_param_set_version (layer, scope_kind, scope_id, version),
  KEY idx_param_set_lookup (layer, scope_kind, scope_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS param_drafts (
  layer VARCHAR(20) NOT NULL,
  scope_kind VARCHAR(40) NOT NULL,
  scope_id VARCHAR(64) NOT NULL,
  fields JSON NOT NULL,
  revision BIGINT NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  updated_by VARCHAR(64) NOT NULL DEFAULT '',
  PRIMARY KEY (layer, scope_kind, scope_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sites (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(200) NOT NULL,
  rain_region_id VARCHAR(64) NOT NULL DEFAULT '',
  remark VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_sites_rain_region (rain_region_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS endpoints (
  id VARCHAR(64) PRIMARY KEY,
  site_id VARCHAR(64) NOT NULL,
  name VARCHAR(200) NOT NULL,
  radio_model_id VARCHAR(64) NOT NULL,
  radio_batch_id VARCHAR(64) NOT NULL DEFAULT '',
  antenna_model_id VARCHAR(64) NOT NULL,
  antenna_batch_id VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_endpoint_site (site_id),
  KEY idx_endpoint_radio_model (radio_model_id),
  KEY idx_endpoint_radio_batch (radio_batch_id),
  KEY idx_endpoint_antenna_model (antenna_model_id),
  KEY idx_endpoint_antenna_batch (antenna_batch_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS rain_climates (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(200) NOT NULL,
  r001 DOUBLE NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS rain_band_coefficients (
  id VARCHAR(64) PRIMARY KEY,
  band_id VARCHAR(64) NOT NULL,
  frequency_ghz DOUBLE NOT NULL,
  polarization VARCHAR(4) NOT NULL,
  k DOUBLE NOT NULL,
  alpha DOUBLE NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  UNIQUE KEY uq_rain_coefficient (band_id, polarization)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS links (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(200) NOT NULL,
  end_a_id VARCHAR(64) NOT NULL,
  end_b_id VARCHAR(64) NOT NULL,
  site_a_id VARCHAR(64) NOT NULL,
  site_b_id VARCHAR(64) NOT NULL,
  frequency_ghz DOUBLE NOT NULL,
  distance_km DOUBLE NOT NULL,
  polarization VARCHAR(4) NOT NULL,
  rain_region_id VARCHAR(64) NOT NULL DEFAULT '',
  rain_band_id VARCHAR(64) NOT NULL DEFAULT '',
  target_availability DOUBLE NOT NULL DEFAULT 0,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  KEY idx_link_end_a (end_a_id),
  KEY idx_link_end_b (end_b_id),
  KEY idx_link_site_a (site_a_id),
  KEY idx_link_site_b (site_b_id),
  CONSTRAINT chk_link_ends CHECK (end_a_id <> end_b_id),
  CONSTRAINT chk_link_sites CHECK (site_a_id <> site_b_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS channels (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(200) NOT NULL,
  target_availability DOUBLE NOT NULL,
  correlation_rho DOUBLE NOT NULL DEFAULT 0.6,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS channel_links (
  channel_id VARCHAR(64) NOT NULL,
  position INT NOT NULL,
  link_id VARCHAR(64) NOT NULL,
  PRIMARY KEY (channel_id, position),
  UNIQUE KEY uq_channel_link_position (channel_id, position),
  KEY idx_channel_link_link (link_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS link_evaluations (
  id VARCHAR(64) PRIMARY KEY,
  link_id VARCHAR(64) NOT NULL,
  kind VARCHAR(30) NOT NULL,
  task_id VARCHAR(64) NOT NULL DEFAULT '',
  evaluated_at DATETIME(6) NOT NULL,
  layer_versions JSON NOT NULL,
  input_hash VARCHAR(64) NOT NULL,
  payload JSON NOT NULL,
  KEY idx_link_eval_link_time (link_id, evaluated_at),
  KEY idx_link_eval_task (task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS channel_evaluations (
  id VARCHAR(64) PRIMARY KEY,
  channel_id VARCHAR(64) NOT NULL,
  kind VARCHAR(30) NOT NULL,
  task_id VARCHAR(64) NOT NULL DEFAULT '',
  evaluated_at DATETIME(6) NOT NULL,
  layer_versions JSON NOT NULL,
  input_hash VARCHAR(64) NOT NULL,
  payload JSON NOT NULL,
  KEY idx_channel_eval_channel_time (channel_id, evaluated_at),
  KEY idx_channel_eval_task (task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS recompute_tasks (
  id VARCHAR(64) PRIMARY KEY,
  status VARCHAR(20) NOT NULL,
  reason VARCHAR(500) NOT NULL DEFAULT '',
  trigger_source VARCHAR(20) NOT NULL,
  baseline JSON NOT NULL,
  target JSON NOT NULL,
  progress JSON NOT NULL,
  submitted_at DATETIME(6) NOT NULL,
  started_at DATETIME(6) NULL,
  finished_at DATETIME(6) NULL,
  error_text VARCHAR(1000) NOT NULL DEFAULT '',
  affected JSON NOT NULL,
  diff JSON NOT NULL,
  followup_of VARCHAR(64) NOT NULL DEFAULT '',
  KEY idx_task_status_submitted (status, submitted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Frozen resolution references for a task. A later publication inserts new
-- param_sets rows but never mutates these versions, so a running task uses
-- exactly the layer versions captured when it was submitted.
CREATE TABLE IF NOT EXISTS task_endpoint_snapshots (
  task_id VARCHAR(64) NOT NULL,
  endpoint_id VARCHAR(64) NOT NULL,
  radio_nominal_version BIGINT NOT NULL,
  radio_batch_version BIGINT NOT NULL,
  radio_site_version BIGINT NOT NULL,
  antenna_nominal_version BIGINT NOT NULL,
  antenna_batch_version BIGINT NOT NULL,
  antenna_site_version BIGINT NOT NULL,
  PRIMARY KEY (task_id, endpoint_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
