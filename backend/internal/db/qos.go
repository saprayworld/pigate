package db

import (
	"database/sql"
	"fmt"
	"time"

	"pigate/internal/model"

	"github.com/google/uuid"
)

// ─── QoS Repository Methods ───────────────────────────────────────────────────

// GetQosRules retrieves all QoS rules ordered by priority ascending.
func (r *Repository) GetQosRules() ([]model.QosRule, error) {
	rows, err := r.db.Query(`
		SELECT id, name, interface, match_src_ip, match_dst_ip,
		       egress_rate_mbps, egress_ceil_mbps,
		       ingress_rate_mbps, ingress_ceil_mbps,
		       priority, status, description
		FROM qos_rules
		ORDER BY priority ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query qos_rules: %w", err)
	}
	defer rows.Close()

	var rules []model.QosRule
	for rows.Next() {
		var rule model.QosRule
		var statusInt int
		err := rows.Scan(
			&rule.ID, &rule.Name, &rule.Interface,
			&rule.MatchSrcIP, &rule.MatchDstIP,
			&rule.EgressRateMbps, &rule.EgressCeilMbps,
			&rule.IngressRateMbps, &rule.IngressCeilMbps,
			&rule.Priority, &statusInt, &rule.Description,
		)
		if err != nil {
			return nil, fmt.Errorf("scan qos_rule: %w", err)
		}
		rule.Status = statusInt == 1
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// GetQosRuleByID retrieves a single QoS rule by its ID.
func (r *Repository) GetQosRuleByID(id string) (*model.QosRule, error) {
	row := r.db.QueryRow(`
		SELECT id, name, interface, match_src_ip, match_dst_ip,
		       egress_rate_mbps, egress_ceil_mbps,
		       ingress_rate_mbps, ingress_ceil_mbps,
		       priority, status, description
		FROM qos_rules
		WHERE id = ?
	`, id)

	var rule model.QosRule
	var statusInt int
	err := row.Scan(
		&rule.ID, &rule.Name, &rule.Interface,
		&rule.MatchSrcIP, &rule.MatchDstIP,
		&rule.EgressRateMbps, &rule.EgressCeilMbps,
		&rule.IngressRateMbps, &rule.IngressCeilMbps,
		&rule.Priority, &statusInt, &rule.Description,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("qos rule %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get qos rule %q: %w", id, err)
	}
	rule.Status = statusInt == 1
	return &rule, nil
}

// GetQosRulesByInterface retrieves all QoS rules for a specific interface.
func (r *Repository) GetQosRulesByInterface(ifaceName string) ([]model.QosRule, error) {
	rows, err := r.db.Query(`
		SELECT id, name, interface, match_src_ip, match_dst_ip,
		       egress_rate_mbps, egress_ceil_mbps,
		       ingress_rate_mbps, ingress_ceil_mbps,
		       priority, status, description
		FROM qos_rules
		WHERE interface = ?
		ORDER BY priority ASC
	`, ifaceName)
	if err != nil {
		return nil, fmt.Errorf("query qos_rules for iface %s: %w", ifaceName, err)
	}
	defer rows.Close()

	var rules []model.QosRule
	for rows.Next() {
		var rule model.QosRule
		var statusInt int
		err := rows.Scan(
			&rule.ID, &rule.Name, &rule.Interface,
			&rule.MatchSrcIP, &rule.MatchDstIP,
			&rule.EgressRateMbps, &rule.EgressCeilMbps,
			&rule.IngressRateMbps, &rule.IngressCeilMbps,
			&rule.Priority, &statusInt, &rule.Description,
		)
		if err != nil {
			return nil, fmt.Errorf("scan qos_rule: %w", err)
		}
		rule.Status = statusInt == 1
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// CreateQosRule inserts a new QoS rule and returns the created record.
func (r *Repository) CreateQosRule(input model.QosRuleInput) (*model.QosRule, error) {
	id := "qos-" + uuid.New().String()
	statusInt := 0
	if input.Status {
		statusInt = 1
	}

	_, err := r.db.Exec(`
		INSERT INTO qos_rules (
			id, name, interface, match_src_ip, match_dst_ip,
			egress_rate_mbps, egress_ceil_mbps,
			ingress_rate_mbps, ingress_ceil_mbps,
			priority, status, description, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		id, input.Name, input.Interface,
		input.MatchSrcIP, input.MatchDstIP,
		input.EgressRateMbps, input.EgressCeilMbps,
		input.IngressRateMbps, input.IngressCeilMbps,
		input.Priority, statusInt, input.Description,
		time.Now().UTC(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert qos_rule: %w", err)
	}
	return r.GetQosRuleByID(id)
}

// UpdateQosRule updates an existing QoS rule and returns the updated record.
func (r *Repository) UpdateQosRule(id string, input model.QosRuleInput) (*model.QosRule, error) {
	statusInt := 0
	if input.Status {
		statusInt = 1
	}

	res, err := r.db.Exec(`
		UPDATE qos_rules
		SET name = ?, interface = ?, match_src_ip = ?, match_dst_ip = ?,
		    egress_rate_mbps = ?, egress_ceil_mbps = ?,
		    ingress_rate_mbps = ?, ingress_ceil_mbps = ?,
		    priority = ?, status = ?, description = ?
		WHERE id = ?
	`,
		input.Name, input.Interface,
		input.MatchSrcIP, input.MatchDstIP,
		input.EgressRateMbps, input.EgressCeilMbps,
		input.IngressRateMbps, input.IngressCeilMbps,
		input.Priority, statusInt, input.Description,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("update qos_rule %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, fmt.Errorf("qos rule %q not found", id)
	}
	return r.GetQosRuleByID(id)
}

// DeleteQosRule removes a QoS rule by ID.
func (r *Repository) DeleteQosRule(id string) error {
	res, err := r.db.Exec(`DELETE FROM qos_rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete qos_rule %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("qos rule %q not found", id)
	}
	return nil
}

// ToggleQosRuleStatus flips the enabled/disabled status of a QoS rule.
func (r *Repository) ToggleQosRuleStatus(id string) error {
	res, err := r.db.Exec(`
		UPDATE qos_rules SET status = CASE WHEN status = 1 THEN 0 ELSE 1 END WHERE id = ?
	`, id)
	if err != nil {
		return fmt.Errorf("toggle qos_rule status %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("qos rule %q not found", id)
	}
	return nil
}

// DisableQosRulesByInterface sets all rules on a specific interface to disabled.
func (r *Repository) DisableQosRulesByInterface(ifaceName string) error {
	_, err := r.db.Exec(`UPDATE qos_rules SET status = 0 WHERE interface = ?`, ifaceName)
	if err != nil {
		return fmt.Errorf("disable qos rules for iface %s: %w", ifaceName, err)
	}
	return nil
}
