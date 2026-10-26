"""Acceptance gates for the explicitly authorized performance-first schedule."""


def priority_authorized(window):
    evidence = window.get('priority_authorization') or {}
    return (window.get('requested') is True
            and window.get('priority') == 'performance_before_remaining_api'
            and evidence.get('independently_verified') is True
            and evidence.get('answer') == 'Run the #406 performance comparison first'
            and evidence.get('source_thread') == window.get('authorized_source_thread')
            and bool(evidence.get('user_message_id')))


def checkpoint_allowed(scan, window):
    return (priority_authorized(window)
            and scan.get('status') == 'paused_for_performance'
            and scan.get('no_active_scan_jobs') is True
            and bool(scan.get('checkpoint')))


def api_accepted(scan):
    return (scan.get('status') == 'complete' and scan.get('trial_count') == 18
            and scan.get('valid_pairs') == 9)


def api_settled(scan):
    return (api_accepted(scan) or
            (scan.get('status') == 'incomplete_budget' and scan.get('attempts_complete') is True
             and scan.get('trial_count') == 18 and scan.get('no_active_scan_jobs') is True))


def final_api_status(report, deadline_reached):
    """Budget exhaustion cannot hide an unrelated execution/correctness failure."""
    if report.get('complete'):
        return 'complete'
    if not deadline_reached or not report.get('attempts_complete'):
        return 'failed'
    for trial in report['trials']:
        error = trial.get('error_type')
        if error and not (error == 'BudgetDeadline' or
                          (error == 'QueryDeadline' and trial.get('deadline_limited'))):
            return 'failed'
    for pair in report['comparisons']:
        if pair.get('status') not in ('valid', 'failed_trial'):
            if not (pair.get('error_type') == 'QueryDeadline' and pair.get('deadline_limited')):
                return 'failed'
    return 'incomplete_budget'
