#!/bin/bash
# Reporting Library for BoostedBudget Test Scenarios
# Generates HTML and JSON reports for test scenario results

# Source dependencies
if ! type -t log_info &>/dev/null; then
    source "$(dirname "${BASH_SOURCE[0]}")/api-client.sh"
fi

# Report data structure
REPORT_SCENARIO_NAME=""
REPORT_START_TIME=""
REPORT_END_TIME=""
REPORT_STATUS="pending"
REPORT_ASSERTIONS_PASSED=0
REPORT_ASSERTIONS_FAILED=0
REPORT_ASSERTION_FAILURES=()
REPORT_STEPS=()
REPORT_METADATA=()

# ============================================================================
# Report Initialization
# ============================================================================

# Initialize report for a scenario
# Usage: init_report <scenario_name>
init_report() {
    local scenario_name="$1"

    if [[ -z "$scenario_name" ]]; then
        log_error "init_report: scenario_name is required"
        return 1
    fi

    REPORT_SCENARIO_NAME="$scenario_name"
    REPORT_START_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    REPORT_STATUS="running"
    REPORT_ASSERTIONS_PASSED=0
    REPORT_ASSERTIONS_FAILED=0
    REPORT_ASSERTION_FAILURES=()
    REPORT_STEPS=()
    REPORT_METADATA=()

    log_info "Initialized report for scenario: $scenario_name"
}

# Finalize report
# Usage: finalize_report <status>
finalize_report() {
    local status="$1"

    if [[ -z "$status" ]]; then
        log_error "finalize_report: status is required (success|failure)"
        return 1
    fi

    REPORT_END_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    REPORT_STATUS="$status"

    log_info "Finalized report with status: $status"
}

# ============================================================================
# Report Data Collection
# ============================================================================

# Add step to report
# Usage: add_report_step <step_number> <action> <status> [details]
add_report_step() {
    local step_number="$1"
    local action="$2"
    local status="$3"
    local details="${4:-}"

    local step_entry="step:$step_number|action:$action|status:$status|details:$details"
    REPORT_STEPS+=("$step_entry")
}

# Add metadata to report
# Usage: add_report_metadata <key> <value>
add_report_metadata() {
    local key="$1"
    local value="$2"

    local metadata_entry="$key:$value"
    REPORT_METADATA+=("$metadata_entry")
}

# Set assertion counts
# Usage: set_assertion_counts <passed> <failed> [failures_array]
set_assertion_counts() {
    local passed="$1"
    local failed="$2"

    REPORT_ASSERTIONS_PASSED="$passed"
    REPORT_ASSERTIONS_FAILED="$failed"

    # Copy failure messages if provided
    if [[ $# -gt 2 ]]; then
        shift 2
        REPORT_ASSERTION_FAILURES=("$@")
    fi
}

# ============================================================================
# JSON Report Generation
# ============================================================================

# Generate JSON report
# Usage: generate_json_report [output_file]
# Returns: JSON report string (or writes to file)
generate_json_report() {
    local output_file="${1:-}"

    # Calculate duration
    local duration_seconds=0
    if [[ -n "$REPORT_START_TIME" ]] && [[ -n "$REPORT_END_TIME" ]]; then
        local start_epoch=$(date -d "$REPORT_START_TIME" +%s 2>/dev/null || echo 0)
        local end_epoch=$(date -d "$REPORT_END_TIME" +%s 2>/dev/null || echo 0)
        duration_seconds=$((end_epoch - start_epoch))
    fi

    # Build steps JSON array
    local steps_json="["
    local first=true
    for step_entry in "${REPORT_STEPS[@]}"; do
        if [[ "$first" == true ]]; then
            first=false
        else
            steps_json+=","
        fi

        # Parse step entry
        IFS='|' read -r step_part action_part status_part details_part <<< "$step_entry"
        local step_num=$(echo "$step_part" | cut -d':' -f2)
        local action=$(echo "$action_part" | cut -d':' -f2-)
        local status=$(echo "$status_part" | cut -d':' -f2)
        local details=$(echo "$details_part" | cut -d':' -f2-)

        steps_json+=$(cat <<EOF

    {
      "step": $step_num,
      "action": "$action",
      "status": "$status",
      "details": "$details"
    }
EOF
        )
    done
    steps_json+="
  ]"

    # Build failures JSON array
    local failures_json="["
    first=true
    for failure in "${REPORT_ASSERTION_FAILURES[@]}"; do
        if [[ "$first" == true ]]; then
            first=false
        else
            failures_json+=","
        fi
        # Escape quotes in failure message
        local escaped_failure="${failure//\"/\\\"}"
        failures_json+="
    \"$escaped_failure\""
    done
    failures_json+="
  ]"

    # Build metadata JSON object
    local metadata_json="{"
    first=true
    for metadata_entry in "${REPORT_METADATA[@]}"; do
        if [[ "$first" == true ]]; then
            first=false
        else
            metadata_json+=","
        fi
        local key=$(echo "$metadata_entry" | cut -d':' -f1)
        local value=$(echo "$metadata_entry" | cut -d':' -f2-)
        metadata_json+="
    \"$key\": \"$value\""
    done
    metadata_json+="
  }"

    # Build full JSON report
    local json_report=$(cat <<EOF
{
  "scenario": "$REPORT_SCENARIO_NAME",
  "status": "$REPORT_STATUS",
  "start_time": "$REPORT_START_TIME",
  "end_time": "$REPORT_END_TIME",
  "duration_seconds": $duration_seconds,
  "assertions": {
    "total": $((REPORT_ASSERTIONS_PASSED + REPORT_ASSERTIONS_FAILED)),
    "passed": $REPORT_ASSERTIONS_PASSED,
    "failed": $REPORT_ASSERTIONS_FAILED,
    "failures": $failures_json
  },
  "steps": $steps_json,
  "metadata": $metadata_json
}
EOF
    )

    # Output to file or stdout
    if [[ -n "$output_file" ]]; then
        echo "$json_report" > "$output_file"
        log_success "JSON report written to: $output_file"
    else
        echo "$json_report"
    fi
}

# ============================================================================
# HTML Report Generation
# ============================================================================

# Generate HTML report
# Usage: generate_html_report [output_file]
# Returns: HTML report string (or writes to file)
generate_html_report() {
    local output_file="${1:-}"

    # Calculate duration
    local duration_seconds=0
    if [[ -n "$REPORT_START_TIME" ]] && [[ -n "$REPORT_END_TIME" ]]; then
        local start_epoch=$(date -d "$REPORT_START_TIME" +%s 2>/dev/null || echo 0)
        local end_epoch=$(date -d "$REPORT_END_TIME" +%s 2>/dev/null || echo 0)
        duration_seconds=$((end_epoch - start_epoch))
    fi

    # Determine status color
    local status_color="orange"
    if [[ "$REPORT_STATUS" == "success" ]]; then
        status_color="green"
    elif [[ "$REPORT_STATUS" == "failure" ]]; then
        status_color="red"
    fi

    # Build steps HTML
    local steps_html=""
    for step_entry in "${REPORT_STEPS[@]}"; do
        # Parse step entry
        IFS='|' read -r step_part action_part status_part details_part <<< "$step_entry"
        local step_num=$(echo "$step_part" | cut -d':' -f2)
        local action=$(echo "$action_part" | cut -d':' -f2-)
        local status=$(echo "$status_part" | cut -d':' -f2)
        local details=$(echo "$details_part" | cut -d':' -f2-)

        local step_color="green"
        local step_icon="✓"
        if [[ "$status" == "failed" ]]; then
            step_color="red"
            step_icon="✗"
        elif [[ "$status" == "pending" ]]; then
            step_color="orange"
            step_icon="⏳"
        fi

        steps_html+=$(cat <<EOF

        <tr>
          <td>$step_num</td>
          <td>$action</td>
          <td style="color: $step_color; font-weight: bold;">$step_icon $status</td>
          <td>$details</td>
        </tr>
EOF
        )
    done

    # Build failures HTML
    local failures_html=""
    if [[ ${#REPORT_ASSERTION_FAILURES[@]} -gt 0 ]]; then
        failures_html="<h2 style=\"color: red;\">Failed Assertions</h2><ul>"
        for failure in "${REPORT_ASSERTION_FAILURES[@]}"; do
            failures_html+="<li>$failure</li>"
        done
        failures_html+="</ul>"
    fi

    # Build metadata HTML
    local metadata_html=""
    if [[ ${#REPORT_METADATA[@]} -gt 0 ]]; then
        metadata_html="<h2>Metadata</h2><table border=\"1\" cellpadding=\"5\" cellspacing=\"0\"><tr><th>Key</th><th>Value</th></tr>"
        for metadata_entry in "${REPORT_METADATA[@]}"; do
            local key=$(echo "$metadata_entry" | cut -d':' -f1)
            local value=$(echo "$metadata_entry" | cut -d':' -f2-)
            metadata_html+="<tr><td>$key</td><td>$value</td></tr>"
        done
        metadata_html+="</table>"
    fi

    # Build full HTML report
    local html_report=$(cat <<EOF
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Test Scenario Report - $REPORT_SCENARIO_NAME</title>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
      max-width: 1200px;
      margin: 0 auto;
      padding: 20px;
      background: #f5f5f5;
    }
    .container {
      background: white;
      padding: 30px;
      border-radius: 8px;
      box-shadow: 0 2px 4px rgba(0,0,0,0.1);
    }
    h1 {
      color: #333;
      border-bottom: 3px solid #007bff;
      padding-bottom: 10px;
    }
    h2 {
      color: #555;
      margin-top: 30px;
    }
    .status {
      display: inline-block;
      padding: 8px 16px;
      border-radius: 4px;
      font-weight: bold;
      color: white;
      background-color: $status_color;
    }
    .stats {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
      gap: 20px;
      margin: 20px 0;
    }
    .stat-box {
      padding: 20px;
      border-radius: 8px;
      background: #f8f9fa;
      border-left: 4px solid #007bff;
    }
    .stat-label {
      font-size: 14px;
      color: #666;
      margin-bottom: 5px;
    }
    .stat-value {
      font-size: 28px;
      font-weight: bold;
      color: #333;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      margin: 20px 0;
    }
    th {
      background: #007bff;
      color: white;
      padding: 12px;
      text-align: left;
    }
    td {
      padding: 12px;
      border-bottom: 1px solid #ddd;
    }
    tr:hover {
      background: #f8f9fa;
    }
    ul {
      line-height: 1.8;
    }
  </style>
</head>
<body>
  <div class="container">
    <h1>Test Scenario Report</h1>
    <h2>$REPORT_SCENARIO_NAME</h2>
    <p><span class="status">$REPORT_STATUS</span></p>

    <div class="stats">
      <div class="stat-box">
        <div class="stat-label">Duration</div>
        <div class="stat-value">${duration_seconds}s</div>
      </div>
      <div class="stat-box">
        <div class="stat-label">Assertions Passed</div>
        <div class="stat-value" style="color: green;">$REPORT_ASSERTIONS_PASSED</div>
      </div>
      <div class="stat-box">
        <div class="stat-label">Assertions Failed</div>
        <div class="stat-value" style="color: red;">$REPORT_ASSERTIONS_FAILED</div>
      </div>
      <div class="stat-box">
        <div class="stat-label">Total Assertions</div>
        <div class="stat-value">$((REPORT_ASSERTIONS_PASSED + REPORT_ASSERTIONS_FAILED))</div>
      </div>
    </div>

    <h2>Test Steps</h2>
    <table>
      <tr>
        <th>Step</th>
        <th>Action</th>
        <th>Status</th>
        <th>Details</th>
      </tr>
      $steps_html
    </table>

    $failures_html

    $metadata_html

    <div style="margin-top: 40px; padding-top: 20px; border-top: 1px solid #ddd; color: #666; font-size: 12px;">
      <p>Start Time: $REPORT_START_TIME</p>
      <p>End Time: $REPORT_END_TIME</p>
      <p>Generated by BoostedBudget Test Scenarios Framework</p>
    </div>
  </div>
</body>
</html>
EOF
    )

    # Output to file or stdout
    if [[ -n "$output_file" ]]; then
        echo "$html_report" > "$output_file"
        log_success "HTML report written to: $output_file"
    else
        echo "$html_report"
    fi
}

# ============================================================================
# Report Summary
# ============================================================================

# Print report summary to console
# Usage: print_report_summary
print_report_summary() {
    local duration_seconds=0
    if [[ -n "$REPORT_START_TIME" ]] && [[ -n "$REPORT_END_TIME" ]]; then
        local start_epoch=$(date -d "$REPORT_START_TIME" +%s 2>/dev/null || echo 0)
        local end_epoch=$(date -d "$REPORT_END_TIME" +%s 2>/dev/null || echo 0)
        duration_seconds=$((end_epoch - start_epoch))
    fi

    echo ""
    echo "========================================"
    echo "     SCENARIO EXECUTION SUMMARY"
    echo "========================================"
    echo "Scenario:  $REPORT_SCENARIO_NAME"
    echo "Status:    $REPORT_STATUS"
    echo "Duration:  ${duration_seconds}s"
    echo ""
    echo "Assertions:"
    echo "  Total:   $((REPORT_ASSERTIONS_PASSED + REPORT_ASSERTIONS_FAILED))"
    echo -e "  ${GREEN}Passed:  $REPORT_ASSERTIONS_PASSED${NC}"
    echo -e "  ${RED}Failed:  $REPORT_ASSERTIONS_FAILED${NC}"
    echo "========================================"
    echo ""
}

# ============================================================================
# Export Functions
# ============================================================================

export -f init_report
export -f finalize_report
export -f add_report_step
export -f add_report_metadata
export -f set_assertion_counts
export -f generate_json_report
export -f generate_html_report
export -f print_report_summary

# Export report variables
export REPORT_SCENARIO_NAME
export REPORT_START_TIME
export REPORT_END_TIME
export REPORT_STATUS
export REPORT_ASSERTIONS_PASSED
export REPORT_ASSERTIONS_FAILED
export REPORT_ASSERTION_FAILURES
export REPORT_STEPS
export REPORT_METADATA
