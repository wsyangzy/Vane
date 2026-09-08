export namespace logging {

	export class LogEntry {
	    timestamp: string;
	    level: string;
	    message: string;
	    attrs: Record<string, string>;

	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.level = source["level"];
	        this.message = source["message"];
	        this.attrs = source["attrs"];
	    }
	}

}

export namespace privacy {

	export class Settings {
	    mode: string;
	    scan_max_bytes: number;
	    over_limit_action: string;
	    on_error: string;
	    unsupported_path_action: string;

	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.scan_max_bytes = source["scan_max_bytes"];
	        this.over_limit_action = source["over_limit_action"];
	        this.on_error = source["on_error"];
	        this.unsupported_path_action = source["unsupported_path_action"];
	    }
	}
	export class Rule {
	    id: number;
	    enabled: boolean;
	    name: string;
	    description: string;
	    priority: number;
	    match_type: string;
	    pattern: string;
	    action: string;
	    replacement_mode: string;
	    placeholder: string;
	    pseudonym_kind: string;
	    source: string;

	    static createFrom(source: any = {}) {
	        return new Rule(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.enabled = source["enabled"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.priority = source["priority"];
	        this.match_type = source["match_type"];
	        this.pattern = source["pattern"];
	        this.action = source["action"];
	        this.replacement_mode = source["replacement_mode"];
	        this.placeholder = source["placeholder"];
	        this.pseudonym_kind = source["pseudonym_kind"];
	        this.source = source["source"];
	    }
	}
	export class StoredRule extends Rule {
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new StoredRule(source);
	    }

	    constructor(source: any = {}) {
	        super(source);
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	// 旧版规则只读快照。pattern_masked 已由后端掩码，前端拿不到敏感原值。
	export class LegacyRule {
	    id: number;
	    name: string;
	    type: string;
	    category: string;
	    pattern_masked: string;
	    value_length: number;
	    replacement_mode: string;
	    enabled: boolean;
	    priority: number;

	    static createFrom(source: any = {}) {
	        return new LegacyRule(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.category = source["category"];
	        this.pattern_masked = source["pattern_masked"];
	        this.value_length = source["value_length"];
	        this.replacement_mode = source["replacement_mode"];
	        this.enabled = source["enabled"];
	        this.priority = source["priority"];
	    }
	}
	// 命中统计。命中原文在 Go 侧带 json:"-"，不会出现在这里。
	export class RuleHit {
	    rule_id: number;
	    rule_name: string;
	    source: string;
	    action: string;
	    count: number;

	    static createFrom(source: any = {}) {
	        return new RuleHit(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule_id = source["rule_id"];
	        this.rule_name = source["rule_name"];
	        this.source = source["source"];
	        this.action = source["action"];
	        this.count = source["count"];
	    }
	}
	export class UnscannedPathStat {
	    path: string;
	    count: number;
	    last_seen_at: string;

	    static createFrom(source: any = {}) {
	        return new UnscannedPathStat(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.count = source["count"];
	        this.last_seen_at = source["last_seen_at"];
	    }
	}
	export class Preset {
	    id: string;
	    name: string;
	    description: string;
	    rules: Rule[];

	    static createFrom(source: any = {}) {
	        return new Preset(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.rules = source["rules"];
	    }
	}
	export class PreviewResult {
	    output: string;
	    changed: boolean;
	    hit_count: number;
	    rule_hits: RuleHit[];

	    static createFrom(source: any = {}) {
	        return new PreviewResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.output = source["output"];
	        this.changed = source["changed"];
	        this.hit_count = source["hit_count"];
	        this.rule_hits = source["rule_hits"];
	    }
	}
	export class Overview {
	    settings: Settings;
	    rules: StoredRule[];
	    legacy_rules: LegacyRule[];
	    unscanned_paths: UnscannedPathStat[];
	    presets: Preset[];
	    pseudonym_kinds: string[];

	    static createFrom(source: any = {}) {
	        return new Overview(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = source["settings"];
	        this.rules = source["rules"];
	        this.legacy_rules = source["legacy_rules"];
	        this.unscanned_paths = source["unscanned_paths"];
	        this.presets = source["presets"];
	        this.pseudonym_kinds = source["pseudonym_kinds"];
	    }
	}

}

export namespace main {

	export class BatchHealthCheckResult {
	    success: boolean;
	    message: string;
	    total: number;
	    healthy_count: number;
	    unhealthy_count: number;

	    static createFrom(source: any = {}) {
	        return new BatchHealthCheckResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.total = source["total"];
	        this.healthy_count = source["healthy_count"];
	        this.unhealthy_count = source["unhealthy_count"];
	    }
	}
	export class CacheStatusItem {
	    name: string;
	    enabled: boolean;
	    ttl: string;
	    max_entries: number;
	    size: number;
	    hits: number;
	    misses: number;
	    expired: number;
	    evicted: number;
	    invalidations: number;
	    last_updated_at: string;
	    last_invalidated_at: string;

	    static createFrom(source: any = {}) {
	        return new CacheStatusItem(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	        this.ttl = source["ttl"];
	        this.max_entries = source["max_entries"];
	        this.size = source["size"];
	        this.hits = source["hits"];
	        this.misses = source["misses"];
	        this.expired = source["expired"];
	        this.evicted = source["evicted"];
	        this.invalidations = source["invalidations"];
	        this.last_updated_at = source["last_updated_at"];
	        this.last_invalidated_at = source["last_invalidated_at"];
	    }
	}
	export class CacheStatus {
	    enabled: boolean;
	    caches: CacheStatusItem[];

	    static createFrom(source: any = {}) {
	        return new CacheStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.caches = this.convertValues(source["caches"], CacheStatusItem);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ClearCacheInput {
	    names: string[];

	    static createFrom(source: any = {}) {
	        return new ClearCacheInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.names = source["names"];
	    }
	}
	export class CacheClearResult {
	    cleared_caches: number;
	    cleared_entries: number;
	    unknown_names?: string[];

	    static createFrom(source: any = {}) {
	        return new CacheClearResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cleared_caches = source["cleared_caches"];
	        this.cleared_entries = source["cleared_entries"];
	        this.unknown_names = source["unknown_names"];
	    }
	}
export class UpdateSettingInput {
	    provider: string;
	    category: string;
	    key: string;
	    value: string;

	    static createFrom(source: any = {}) {
	        return new UpdateSettingInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.category = source["category"];
	        this.key = source["key"];
	        this.value = source["value"];
	    }
	}
	export class ScopedSettingsInput {
	    provider: string;
	    category: string;

	    static createFrom(source: any = {}) { return new ScopedSettingsInput(source); }
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.category = source["category"];
	    }
	}
	export class BatchUpdateSettingsInput {
	    settings: UpdateSettingInput[];

	    static createFrom(source: any = {}) {
	        return new BatchUpdateSettingsInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], UpdateSettingInput);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CategoryInfo {
	    name: string;
	    label: string;
	    description: string;
	    icon: string;
	    order: number;

	    static createFrom(source: any = {}) {
	        return new CategoryInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.icon = source["icon"];
	        this.order = source["order"];
	    }
	}
	export class ChannelInfo {
	    name: string;
	    website?: string;
	    priority: number;
	    model_mapping: Record<string, string>;
	    created_at: string;
	    endpoint_count: number;
	    credential_groups: ClaudeCredentialGroupInfo[];

	    static createFrom(source: any = {}) {
	        return new ChannelInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.website = source["website"];
	        this.priority = source["priority"];
	        this.model_mapping = source["model_mapping"];
	        this.created_at = source["created_at"];
	        this.endpoint_count = source["endpoint_count"];
	        this.credential_groups = (source["credential_groups"] || []).map((e: any) => new ClaudeCredentialGroupInfo(e));
	    }
	}
	export class ClaudeCredentialGroupInfo {
	    id: number;
	    channel: string;
	    name: string;
	    token: string;
	    api_key: string;
	    token_masked: string;
	    api_key_masked: string;
	    is_default: boolean;
	    daily_cost_limit_usd: number;
	    cost_multiplier: number;
	    input_cost_multiplier: number;
	    output_cost_multiplier: number;
	    cache_creation_cost_multiplier: number;
	    cache_creation_cost_multiplier_1h: number;
	    cache_read_cost_multiplier: number;
	    model_multipliers: any[];
	    endpoint_count: number;
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new ClaudeCredentialGroupInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.channel = source["channel"];
	        this.name = source["name"];
	        this.token = source["token"];
	        this.api_key = source["api_key"];
	        this.token_masked = source["token_masked"];
	        this.api_key_masked = source["api_key_masked"];
	        this.is_default = source["is_default"];
	        this.daily_cost_limit_usd = source["daily_cost_limit_usd"];
	        this.cost_multiplier = source["cost_multiplier"];
	        this.input_cost_multiplier = source["input_cost_multiplier"];
	        this.output_cost_multiplier = source["output_cost_multiplier"];
	        this.cache_creation_cost_multiplier = source["cache_creation_cost_multiplier"];
	        this.cache_creation_cost_multiplier_1h = source["cache_creation_cost_multiplier_1h"];
	        this.cache_read_cost_multiplier = source["cache_read_cost_multiplier"];
	        this.model_multipliers = source["model_multipliers"];
	        this.endpoint_count = source["endpoint_count"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ChartDataPoint {
	    time: string;
	    total: number;
	    success: number;
	    fail: number;
	    avg: number;
	    min: number;
	    max: number;
	    value: number;
	    timestamp: string;

	    static createFrom(source: any = {}) {
	        return new ChartDataPoint(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.total = source["total"];
	        this.success = source["success"];
	        this.fail = source["fail"];
	        this.avg = source["avg"];
	        this.min = source["min"];
	        this.max = source["max"];
	        this.value = source["value"];
	        this.timestamp = source["timestamp"];
	    }
	}
	export class CredentialGroupDailyUsage {
	    provider: string;
	    channel: string;
	    group_name: string;
	    used_usd: number;
	    limit_usd: number;
	    remaining_usd: number;
	    reached: boolean;
	    reset_at: string;

	    static createFrom(source: any = {}) {
	        return new CredentialGroupDailyUsage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.channel = source["channel"];
	        this.group_name = source["group_name"];
	        this.used_usd = source["used_usd"];
	        this.limit_usd = source["limit_usd"];
	        this.remaining_usd = source["remaining_usd"];
	        this.reached = source["reached"];
	        this.reset_at = source["reset_at"];
	    }
	}
	export class ConfigInfo {
	    server_host: string;
	    server_port: number;
	    auth_enabled: boolean;
	    proxy_enabled: boolean;
	    tracking_enabled: boolean;
	    failover_enabled: boolean;
	    failover_default_cooldown_seconds: number;
	    endpoint_count: number;

	    static createFrom(source: any = {}) {
	        return new ConfigInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server_host = source["server_host"];
	        this.server_port = source["server_port"];
	        this.auth_enabled = source["auth_enabled"];
	        this.proxy_enabled = source["proxy_enabled"];
	        this.tracking_enabled = source["tracking_enabled"];
	        this.failover_enabled = source["failover_enabled"];
	        this.failover_default_cooldown_seconds = source["failover_default_cooldown_seconds"];
	        this.endpoint_count = source["endpoint_count"];
	    }
	}
	export class CreateChannelInput {
	    name: string;
	    website?: string;
	    priority: number;
	    model_mapping: Record<string, string>;

	    static createFrom(source: any = {}) {
	        return new CreateChannelInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.website = source["website"];
	        this.priority = source["priority"];
	        this.model_mapping = source["model_mapping"];
	    }
	}
	export class CodexProviderInfo {
	    id: number;
	    name: string;
	    provider_type: string;
	    base_url: string;
	    api_key: string;
	    api_key_masked: string;
	    model: string;
	    model_reasoning_effort: string;
	    enable_1m_context: boolean;
	    website: string;
	    is_active: boolean;
	    priority: number;
	    failover_enabled: boolean;
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new CodexProviderInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider_type = source["provider_type"];
	        this.base_url = source["base_url"];
	        this.api_key = source["api_key"];
	        this.api_key_masked = source["api_key_masked"];
	        this.model = source["model"];
	        this.model_reasoning_effort = source["model_reasoning_effort"];
	        this.enable_1m_context = source["enable_1m_context"];
	        this.website = source["website"];
	        this.is_active = source["is_active"];
	        this.priority = source["priority"];
	        this.failover_enabled = source["failover_enabled"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class CodexProviderStorageStatus {
	    enabled: boolean;
	    total_count: number;
	    has_active: boolean;
	    active_provider_id: number;
	    active_provider_name: string;

	    static createFrom(source: any = {}) {
	        return new CodexProviderStorageStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.total_count = source["total_count"];
	        this.has_active = source["has_active"];
	        this.active_provider_id = source["active_provider_id"];
	        this.active_provider_name = source["active_provider_name"];
	    }
	}
	export class CreateCodexProviderInput {
	    name: string;
	    provider_type: string;
	    base_url: string;
	    api_key: string;
	    model: string;
	    model_reasoning_effort: string;
	    enable_1m_context: boolean;
	    website: string;
	    priority: number;
	    failover_enabled?: boolean;

	    static createFrom(source: any = {}) {
	        return new CreateCodexProviderInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.provider_type = source["provider_type"];
	        this.base_url = source["base_url"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.model_reasoning_effort = source["model_reasoning_effort"];
	        this.enable_1m_context = source["enable_1m_context"];
	        this.website = source["website"];
	        this.priority = source["priority"];
	        this.failover_enabled = source["failover_enabled"];
	    }
	}
	// ============ Codex Channel-Endpoint 类型（2026-05-13 新增） ============
	export class CodexEndpointInfo {
	    id: number;
	    channel_id: number;
	    credential_group_id: number;
	    credential_group_name: string;
	    api_key: string;
	    api_key_masked: string;
	    name: string;
	    base_url: string;
	    is_active: boolean;
	    skip_health_check: boolean;
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new CodexEndpointInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.channel_id = source["channel_id"];
	        this.credential_group_id = source["credential_group_id"];
	        this.credential_group_name = source["credential_group_name"];
	        this.api_key = source["api_key"];
	        this.api_key_masked = source["api_key_masked"];
	        this.name = source["name"];
	        this.base_url = source["base_url"];
	        this.is_active = source["is_active"];
	        this.skip_health_check = source["skip_health_check"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class CodexCredentialGroupInfo {
	    id: number;
	    channel_id: number;
	    name: string;
	    api_key: string;
	    api_key_masked: string;
	    is_default: boolean;
	    daily_cost_limit_usd: number;
	    costMultiplier: number;
	    inputCostMultiplier: number;
	    outputCostMultiplier: number;
	    cacheCreationCostMultiplier: number;
	    cacheReadCostMultiplier: number;
	    longContextCostMultiplier: number;
	    longContextInputCostMultiplier: number;
	    longContextOutputCostMultiplier: number;
	    longContextCacheCreationCostMultiplier: number;
	    longContextCacheReadCostMultiplier: number;
	    disableLongContextPricing: boolean;
	    billingOutputTokenSource: string;
	    modelMultipliers: any[];
	    billingRules: any[];
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new CodexCredentialGroupInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.channel_id = source["channel_id"];
	        this.name = source["name"];
	        this.api_key = source["api_key"];
	        this.api_key_masked = source["api_key_masked"];
	        this.is_default = source["is_default"];
	        this.daily_cost_limit_usd = source["daily_cost_limit_usd"];
	        this.costMultiplier = source["costMultiplier"];
	        this.inputCostMultiplier = source["inputCostMultiplier"];
	        this.outputCostMultiplier = source["outputCostMultiplier"];
	        this.cacheCreationCostMultiplier = source["cacheCreationCostMultiplier"];
	        this.cacheReadCostMultiplier = source["cacheReadCostMultiplier"];
	        this.longContextCostMultiplier = source["longContextCostMultiplier"];
	        this.longContextInputCostMultiplier = source["longContextInputCostMultiplier"];
	        this.longContextOutputCostMultiplier = source["longContextOutputCostMultiplier"];
	        this.longContextCacheCreationCostMultiplier = source["longContextCacheCreationCostMultiplier"];
	        this.longContextCacheReadCostMultiplier = source["longContextCacheReadCostMultiplier"];
	        this.disableLongContextPricing = source["disableLongContextPricing"];
	        this.billingOutputTokenSource = source["billingOutputTokenSource"];
	        this.modelMultipliers = source["modelMultipliers"];
	        this.billingRules = source["billingRules"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class CodexChannelInfo {
	    id: number;
	    name: string;
	    provider_type: string;
	    api_key: string;
	    api_key_masked: string;
	    model: string;
	    model_reasoning_effort: string;
	    enable_1m_context: boolean;
	    website: string;
	    priority: number;
	    failover_enabled?: boolean;
	    is_active: boolean;
	    credential_groups: CodexCredentialGroupInfo[];
	    endpoints: CodexEndpointInfo[];
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new CodexChannelInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider_type = source["provider_type"];
	        this.api_key = source["api_key"];
	        this.api_key_masked = source["api_key_masked"];
	        this.model = source["model"];
	        this.model_reasoning_effort = source["model_reasoning_effort"];
	        this.enable_1m_context = source["enable_1m_context"];
	        this.website = source["website"];
	        this.priority = source["priority"];
	        this.failover_enabled = source["failover_enabled"];
	        this.is_active = source["is_active"];
	        this.credential_groups = (source["credential_groups"] || []).map((e: any) => new CodexCredentialGroupInfo(e));
	        this.endpoints = (source["endpoints"] || []).map((e: any) => new CodexEndpointInfo(e));
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class CreateCodexChannelInput {
	    name: string;
	    provider_type: string;
	    api_key: string;
	    model: string;
	    model_reasoning_effort: string;
	    enable_1m_context: boolean;
	    website: string;

	    static createFrom(source: any = {}) {
	        return new CreateCodexChannelInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.provider_type = source["provider_type"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.model_reasoning_effort = source["model_reasoning_effort"];
	        this.enable_1m_context = source["enable_1m_context"];
	        this.website = source["website"];
	    }
	}
	export class UpdateCodexChannelInput {
	    name: string;
	    provider_type: string;
	    api_key: string;
	    model: string;
	    model_reasoning_effort: string;
	    enable_1m_context: boolean;
	    website: string;

	    static createFrom(source: any = {}) {
	        return new UpdateCodexChannelInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.provider_type = source["provider_type"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.model_reasoning_effort = source["model_reasoning_effort"];
	        this.enable_1m_context = source["enable_1m_context"];
	        this.website = source["website"];
	    }
	}
	export class CreateCodexCredentialGroupInput {
	    channel_id: number;
	    name: string;
	    api_key: string;
	    is_default: boolean;
	    daily_cost_limit_usd: number;
	    costMultiplier: number;
	    inputCostMultiplier: number;
	    outputCostMultiplier: number;
	    cacheCreationCostMultiplier: number;
	    cacheReadCostMultiplier: number;
	    longContextCostMultiplier: number;
	    longContextInputCostMultiplier: number;
	    longContextOutputCostMultiplier: number;
	    longContextCacheCreationCostMultiplier: number;
	    longContextCacheReadCostMultiplier: number;
	    disableLongContextPricing: boolean;
	    billingOutputTokenSource: string;
	    modelMultipliers: any[];
	    billingRules: any[];

	    static createFrom(source: any = {}) {
	        return new CreateCodexCredentialGroupInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.channel_id = source["channel_id"];
	        this.name = source["name"];
	        this.api_key = source["api_key"];
	        this.is_default = source["is_default"];
	        this.daily_cost_limit_usd = source["daily_cost_limit_usd"];
	        this.costMultiplier = source["costMultiplier"];
	        this.inputCostMultiplier = source["inputCostMultiplier"];
	        this.outputCostMultiplier = source["outputCostMultiplier"];
	        this.cacheCreationCostMultiplier = source["cacheCreationCostMultiplier"];
	        this.cacheReadCostMultiplier = source["cacheReadCostMultiplier"];
	        this.longContextCostMultiplier = source["longContextCostMultiplier"];
	        this.longContextInputCostMultiplier = source["longContextInputCostMultiplier"];
	        this.longContextOutputCostMultiplier = source["longContextOutputCostMultiplier"];
	        this.longContextCacheCreationCostMultiplier = source["longContextCacheCreationCostMultiplier"];
	        this.longContextCacheReadCostMultiplier = source["longContextCacheReadCostMultiplier"];
	        this.disableLongContextPricing = source["disableLongContextPricing"];
	        this.billingOutputTokenSource = source["billingOutputTokenSource"];
	        this.modelMultipliers = source["modelMultipliers"];
	        this.billingRules = source["billingRules"];
	    }
}
	export class UpdateCodexCredentialGroupInput {
	    name: string;
	    api_key: string;
	    is_default: boolean;
	    daily_cost_limit_usd: number;
	    costMultiplier: number;
	    inputCostMultiplier: number;
	    outputCostMultiplier: number;
	    cacheCreationCostMultiplier: number;
	    cacheReadCostMultiplier: number;
	    longContextCostMultiplier: number;
	    longContextInputCostMultiplier: number;
	    longContextOutputCostMultiplier: number;
	    longContextCacheCreationCostMultiplier: number;
	    longContextCacheReadCostMultiplier: number;
	    disableLongContextPricing: boolean;
	    billingOutputTokenSource: string;
	    modelMultipliers: any[];
	    billingRules: any[];

	    static createFrom(source: any = {}) {
	        return new UpdateCodexCredentialGroupInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.api_key = source["api_key"];
	        this.is_default = source["is_default"];
	        this.daily_cost_limit_usd = source["daily_cost_limit_usd"];
	        this.costMultiplier = source["costMultiplier"];
	        this.inputCostMultiplier = source["inputCostMultiplier"];
	        this.outputCostMultiplier = source["outputCostMultiplier"];
	        this.cacheCreationCostMultiplier = source["cacheCreationCostMultiplier"];
	        this.cacheReadCostMultiplier = source["cacheReadCostMultiplier"];
	        this.longContextCostMultiplier = source["longContextCostMultiplier"];
	        this.longContextInputCostMultiplier = source["longContextInputCostMultiplier"];
	        this.longContextOutputCostMultiplier = source["longContextOutputCostMultiplier"];
	        this.longContextCacheCreationCostMultiplier = source["longContextCacheCreationCostMultiplier"];
	        this.longContextCacheReadCostMultiplier = source["longContextCacheReadCostMultiplier"];
	        this.disableLongContextPricing = source["disableLongContextPricing"];
	        this.billingOutputTokenSource = source["billingOutputTokenSource"];
	        this.modelMultipliers = source["modelMultipliers"];
	        this.billingRules = source["billingRules"];
	    }
}
	export class CreateCodexEndpointInput {
	    channel_id: number;
	    credential_group_id: number;
	    api_key: string;
	    name: string;
	    base_url: string;
	    failover_enabled?: boolean;
	    skip_health_check: boolean;
	    priority?: number;
	    timeout_seconds: number;
	    costMultiplier: number;
	    inputCostMultiplier: number;
	    outputCostMultiplier: number;
	    cacheReadCostMultiplier: number;
	    billingOutputTokenSource: string;
	    modelMultipliers: any[];

	    static createFrom(source: any = {}) {
	        return new CreateCodexEndpointInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.channel_id = source["channel_id"];
	        this.credential_group_id = source["credential_group_id"];
	        this.api_key = source["api_key"];
	        this.name = source["name"];
	        this.base_url = source["base_url"];
	        this.failover_enabled = source["failover_enabled"];
	        this.skip_health_check = source["skip_health_check"];
	        this.priority = source["priority"];
	        this.timeout_seconds = source["timeout_seconds"];
	        this.costMultiplier = source["costMultiplier"];
	        this.inputCostMultiplier = source["inputCostMultiplier"];
	        this.outputCostMultiplier = source["outputCostMultiplier"];
	        this.cacheReadCostMultiplier = source["cacheReadCostMultiplier"];
	        this.billingOutputTokenSource = source["billingOutputTokenSource"];
	        this.modelMultipliers = source["modelMultipliers"];
	    }
	}
	export class UpdateCodexEndpointInput {
	    name: string;
	    base_url: string;
	    credential_group_id?: number;
	    api_key?: string;
	    failover_enabled?: boolean;
	    skip_health_check?: boolean;
	    priority?: number;
	    timeout_seconds?: number;
	    costMultiplier?: number;
	    inputCostMultiplier?: number;
	    outputCostMultiplier?: number;
	    cacheReadCostMultiplier?: number;
	    billingOutputTokenSource?: string;
	    modelMultipliers: any[];

	    static createFrom(source: any = {}) {
	        return new UpdateCodexEndpointInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.base_url = source["base_url"];
	        this.credential_group_id = source["credential_group_id"];
	        this.api_key = source["api_key"];
	        this.failover_enabled = source["failover_enabled"];
	        this.skip_health_check = source["skip_health_check"];
	        this.priority = source["priority"];
	        this.timeout_seconds = source["timeout_seconds"];
	        this.costMultiplier = source["costMultiplier"];
	        this.inputCostMultiplier = source["inputCostMultiplier"];
	        this.outputCostMultiplier = source["outputCostMultiplier"];
	        this.cacheReadCostMultiplier = source["cacheReadCostMultiplier"];
	        this.billingOutputTokenSource = source["billingOutputTokenSource"];
	        this.modelMultipliers = source["modelMultipliers"];
	    }
	}
	export class CreateClaudeCredentialGroupInput {
	    channel: string;
	    name: string;
	    token: string;
	    api_key: string;
	    is_default: boolean;
	    daily_cost_limit_usd: number;
	    cost_multiplier: number;
	    input_cost_multiplier: number;
	    output_cost_multiplier: number;
	    cache_creation_cost_multiplier: number;
	    cache_creation_cost_multiplier_1h: number;
	    cache_read_cost_multiplier: number;
	    model_multipliers: any[];

	    static createFrom(source: any = {}) {
	        return new CreateClaudeCredentialGroupInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.channel = source["channel"];
	        this.name = source["name"];
	        this.token = source["token"];
	        this.api_key = source["api_key"];
	        this.is_default = source["is_default"];
	        this.daily_cost_limit_usd = source["daily_cost_limit_usd"];
	        this.cost_multiplier = source["cost_multiplier"];
	        this.input_cost_multiplier = source["input_cost_multiplier"];
	        this.output_cost_multiplier = source["output_cost_multiplier"];
	        this.cache_creation_cost_multiplier = source["cache_creation_cost_multiplier"];
	        this.cache_creation_cost_multiplier_1h = source["cache_creation_cost_multiplier_1h"];
	        this.cache_read_cost_multiplier = source["cache_read_cost_multiplier"];
	        this.model_multipliers = source["model_multipliers"];
	    }
}
	export class CreateEndpointInput {
	    channel: string;
	    credential_group_id: number;
	    name: string;
	    url: string;
	    token: string;
	    api_key: string;
	    headers: Record<string, string>;
	    priority: number;
	    failover_enabled: boolean;
	    skip_health_check: boolean;
	    cooldown_seconds?: number;
	    timeout_seconds: number;
	    supports_count_tokens: boolean;
	    stream_only: boolean;
	    cost_multiplier: number;
	    input_cost_multiplier: number;
	    output_cost_multiplier: number;
	    cache_creation_cost_multiplier: number;
	    cache_creation_cost_multiplier_1h: number;
	    cache_read_cost_multiplier: number;
	    model_multipliers: any[];

	    static createFrom(source: any = {}) {
	        return new CreateEndpointInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.channel = source["channel"];
	        this.credential_group_id = source["credential_group_id"];
	        this.name = source["name"];
	        this.url = source["url"];
	        this.token = source["token"];
	        this.api_key = source["api_key"];
	        this.headers = source["headers"];
	        this.priority = source["priority"];
	        this.failover_enabled = source["failover_enabled"];
	        this.skip_health_check = source["skip_health_check"];
	        this.cooldown_seconds = source["cooldown_seconds"];
	        this.timeout_seconds = source["timeout_seconds"];
	        this.supports_count_tokens = source["supports_count_tokens"];
	        this.stream_only = source["stream_only"];
	        this.cost_multiplier = source["cost_multiplier"];
	        this.input_cost_multiplier = source["input_cost_multiplier"];
	        this.output_cost_multiplier = source["output_cost_multiplier"];
	        this.cache_creation_cost_multiplier = source["cache_creation_cost_multiplier"];
	        this.cache_creation_cost_multiplier_1h = source["cache_creation_cost_multiplier_1h"];
	        this.cache_read_cost_multiplier = source["cache_read_cost_multiplier"];
	        this.model_multipliers = source["model_multipliers"];
	    }
	}
	export class CreateModelPricingInput {
	    id: number;
	    model_name: string;
	    provider: string;
	    service_tier: string;
	    display_name: string;
	    description: string;
	    input_price: number;
	    output_price: number;
	    cache_creation_price_5m: number;
	    cache_creation_price_1h: number;
	    cache_read_price: number;
	    long_context_threshold: number;
	    long_context_input_price: number;
	    long_context_cache_read_price: number;
	    long_context_cache_write_price: number;
	    long_context_output_price: number;
	    is_default: boolean;

	    static createFrom(source: any = {}) {
	        return new CreateModelPricingInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.model_name = source["model_name"];
	        this.provider = source["provider"];
	        this.service_tier = source["service_tier"];
	        this.display_name = source["display_name"];
	        this.description = source["description"];
	        this.input_price = source["input_price"];
	        this.output_price = source["output_price"];
	        this.cache_creation_price_5m = source["cache_creation_price_5m"];
	        this.cache_creation_price_1h = source["cache_creation_price_1h"];
	        this.cache_read_price = source["cache_read_price"];
	        this.long_context_threshold = source["long_context_threshold"];
	        this.long_context_input_price = source["long_context_input_price"];
	        this.long_context_cache_read_price = source["long_context_cache_read_price"];
	        this.long_context_cache_write_price = source["long_context_cache_write_price"];
	        this.long_context_output_price = source["long_context_output_price"];
	        this.is_default = source["is_default"];
	    }
	}
	export class UpdateCodexProviderInput {
	    name: string;
	    provider_type: string;
	    base_url: string;
	    api_key: string;
	    model: string;
	    model_reasoning_effort: string;
	    enable_1m_context: boolean;
	    website: string;

	    static createFrom(source: any = {}) {
	        return new UpdateCodexProviderInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.provider_type = source["provider_type"];
	        this.base_url = source["base_url"];
	        this.api_key = source["api_key"];
	        this.model = source["model"];
	        this.model_reasoning_effort = source["model_reasoning_effort"];
	        this.enable_1m_context = source["enable_1m_context"];
	        this.website = source["website"];
	    }
	}
	export class EndpointCostItem {
	    name: string;
	    channel: string;
	    endpoint: string;
	    group: string;
	    provider: string;
	    tokens: number;
	    cost: number;
	    provider: string;

	    static createFrom(source: any = {}) {
	        return new EndpointCostItem(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.channel = source["channel"];
	        this.endpoint = source["endpoint"];
	        this.group = source["group"];
	        this.tokens = source["tokens"];
	        this.cost = source["cost"];
	        this.provider = source["provider"];
	    }
	}
	export class EndpointAvailabilitySlice {
	    key: string;
	    label: string;
	    has_data: boolean;
	    total: number;
	    success: number;
	    failed: number;
	    availability: number;
	    avg_first_byte_ms: number;

	    static createFrom(source: any = {}) {
	        return new EndpointAvailabilitySlice(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.has_data = source["has_data"];
	        this.total = source["total"];
	        this.success = source["success"];
	        this.failed = source["failed"];
	        this.availability = source["availability"];
	        this.avg_first_byte_ms = source["avg_first_byte_ms"];
	    }
	}
	export class EndpointAvailabilityData {
	    slices: EndpointAvailabilitySlice[];
	    has_data: boolean;
	    total: number;
	    success: number;
	    failed: number;
	    availability: number;
	    avg_first_byte_ms: number;

	    static createFrom(source: any = {}) {
	        return new EndpointAvailabilityData(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slices = this.convertValues(source["slices"], EndpointAvailabilitySlice);
	        this.has_data = source["has_data"];
	        this.total = source["total"];
	        this.success = source["success"];
	        this.failed = source["failed"];
	        this.availability = source["availability"];
	        this.avg_first_byte_ms = source["avg_first_byte_ms"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EndpointHealthData {
	    healthy: number;
	    unhealthy: number;
	    skipped: number;
	    excluded: number;
	    total: number;

	    static createFrom(source: any = {}) {
	        return new EndpointHealthData(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.healthy = source["healthy"];
	        this.unhealthy = source["unhealthy"];
	        this.skipped = source["skipped"];
	        this.excluded = source["excluded"];
	        this.total = source["total"];
	    }
	}
	export class EndpointInfo {
	    name: string;
	    url: string;
	    channel: string;
	    group: string;
	    priority: number;
	    group_priority: number;
	    group_is_active: boolean;
	    healthy: boolean;
	    health_state: string;
	    skip_health_check: boolean;
	    last_check: string;
	    response_time_ms: number;
	    consecutive_fail: number;
	    last_fail_code: number;
	    last_fail_reason: string;

	    static createFrom(source: any = {}) {
	        return new EndpointInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.url = source["url"];
	        this.channel = source["channel"];
	        this.group = source["group"];
	        this.priority = source["priority"];
	        this.group_priority = source["group_priority"];
	        this.group_is_active = source["group_is_active"];
	        this.healthy = source["healthy"];
	        this.health_state = source["health_state"];
	        this.skip_health_check = source["skip_health_check"];
	        this.last_check = source["last_check"];
	        this.response_time_ms = source["response_time_ms"];
	        this.consecutive_fail = source["consecutive_fail"];
	        this.last_fail_code = source["last_fail_code"];
	        this.last_fail_reason = source["last_fail_reason"];
	    }
	}
	export class KeyInfo {
	    index: number;
	    name: string;
	    value: string;
	    is_active: boolean;

	    static createFrom(source: any = {}) {
	        return new KeyInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.name = source["name"];
	        this.value = source["value"];
	        this.is_active = source["is_active"];
	    }
	}
	export class EndpointKeysInfo {
	    endpoint: string;
	    tokens: KeyInfo[];
	    api_keys: KeyInfo[];
	    current_token_index: number;
	    current_api_key_index: number;

	    static createFrom(source: any = {}) {
	        return new EndpointKeysInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoint = source["endpoint"];
	        this.tokens = this.convertValues(source["tokens"], KeyInfo);
	        this.api_keys = this.convertValues(source["api_keys"], KeyInfo);
	        this.current_token_index = source["current_token_index"];
	        this.current_api_key_index = source["current_api_key_index"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EndpointRecordInfo {
	    id: number;
	    channel: string;
	    credential_group_id: number;
	    credential_group_name: string;
	    name: string;
	    url: string;
	    token: string;
	    api_key: string;
	    token_masked: string;
	    api_key_masked: string;
	    headers: Record<string, string>;
	    priority: number;
	    failover_enabled: boolean;
	    skip_health_check: boolean;
	    cooldown_seconds?: number;
	    timeout_seconds: number;
	    supports_count_tokens: boolean;
	    stream_only: boolean;
	    cost_multiplier: number;
	    input_cost_multiplier: number;
	    output_cost_multiplier: number;
	    cache_creation_cost_multiplier: number;
	    cache_creation_cost_multiplier_1h: number;
	    cache_read_cost_multiplier: number;
	    model_multipliers: any[];
	    enabled: boolean;
	    created_at: string;
	    updated_at: string;
	    healthy: boolean;
	    health_state: string;
	    never_checked: boolean;
	    last_check: string;
	    response_time_ms: number;
	    last_fail_code: number;
	    last_fail_reason: string;
	    in_cooldown: boolean;
	    cooldown_until: string;
	    cooldown_reason: string;

	    static createFrom(source: any = {}) {
	        return new EndpointRecordInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.channel = source["channel"];
	        this.credential_group_id = source["credential_group_id"];
	        this.credential_group_name = source["credential_group_name"];
	        this.name = source["name"];
	        this.url = source["url"];
	        this.token = source["token"];
	        this.api_key = source["api_key"];
	        this.token_masked = source["token_masked"];
	        this.api_key_masked = source["api_key_masked"];
	        this.headers = source["headers"];
	        this.priority = source["priority"];
	        this.failover_enabled = source["failover_enabled"];
	        this.skip_health_check = source["skip_health_check"];
	        this.cooldown_seconds = source["cooldown_seconds"];
	        this.timeout_seconds = source["timeout_seconds"];
	        this.supports_count_tokens = source["supports_count_tokens"];
	        this.stream_only = source["stream_only"];
	        this.cost_multiplier = source["cost_multiplier"];
	        this.input_cost_multiplier = source["input_cost_multiplier"];
	        this.output_cost_multiplier = source["output_cost_multiplier"];
	        this.cache_creation_cost_multiplier = source["cache_creation_cost_multiplier"];
	        this.cache_creation_cost_multiplier_1h = source["cache_creation_cost_multiplier_1h"];
	        this.cache_read_cost_multiplier = source["cache_read_cost_multiplier"];
	        this.model_multipliers = source["model_multipliers"];
	        this.enabled = source["enabled"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	        this.healthy = source["healthy"];
	        this.health_state = source["health_state"];
	        this.never_checked = source["never_checked"];
	        this.last_check = source["last_check"];
	        this.response_time_ms = source["response_time_ms"];
	        this.last_fail_code = source["last_fail_code"];
	        this.last_fail_reason = source["last_fail_reason"];
	        this.in_cooldown = source["in_cooldown"];
	        this.cooldown_until = source["cooldown_until"];
	        this.cooldown_reason = source["cooldown_reason"];
	    }
	}
	export class EndpointStorageStatus {
	    enabled: boolean;
	    storage_type: string;
	    total_count: number;
	    enabled_count: number;

	    static createFrom(source: any = {}) {
	        return new EndpointStorageStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.storage_type = source["storage_type"];
	        this.total_count = source["total_count"];
	        this.enabled_count = source["enabled_count"];
	    }
	}
	export class GroupInfo {
	    name: string;
	    channel: string;
	    active: boolean;
	    paused: boolean;
	    priority: number;
	    endpoint_count: number;
	    in_cooldown: boolean;
	    cooldown_remain_ms: number;

	    static createFrom(source: any = {}) {
	        return new GroupInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.channel = source["channel"];
	        this.active = source["active"];
	        this.paused = source["paused"];
	        this.priority = source["priority"];
	        this.endpoint_count = source["endpoint_count"];
	        this.in_cooldown = source["in_cooldown"];
	        this.cooldown_remain_ms = source["cooldown_remain_ms"];
	    }
	}

	export class KeysOverviewResult {
	    endpoints: EndpointKeysInfo[];
	    total: number;
	    timestamp: string;

	    static createFrom(source: any = {}) {
	        return new KeysOverviewResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.endpoints = this.convertValues(source["endpoints"], EndpointKeysInfo);
	        this.total = source["total"];
	        this.timestamp = source["timestamp"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ModelPricingInfo {
	    id: number;
	    model_name: string;
	    provider: string;
	    service_tier: string;
	    display_name: string;
	    description: string;
	    input_price: number;
	    output_price: number;
	    cache_creation_price_5m: number;
	    cache_creation_price_1h: number;
	    cache_read_price: number;
	    long_context_threshold: number;
	    long_context_input_price: number;
	    long_context_cache_read_price: number;
	    long_context_cache_write_price: number;
	    long_context_output_price: number;
	    is_default: boolean;
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new ModelPricingInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.model_name = source["model_name"];
	        this.provider = source["provider"];
	        this.service_tier = source["service_tier"];
	        this.display_name = source["display_name"];
	        this.description = source["description"];
	        this.input_price = source["input_price"];
	        this.output_price = source["output_price"];
	        this.cache_creation_price_5m = source["cache_creation_price_5m"];
	        this.cache_creation_price_1h = source["cache_creation_price_1h"];
	        this.cache_read_price = source["cache_read_price"];
	        this.long_context_threshold = source["long_context_threshold"];
	        this.long_context_input_price = source["long_context_input_price"];
	        this.long_context_cache_read_price = source["long_context_cache_read_price"];
	        this.long_context_cache_write_price = source["long_context_cache_write_price"];
	        this.long_context_output_price = source["long_context_output_price"];
	        this.is_default = source["is_default"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ModelPricingStorageStatus {
	    enabled: boolean;
	    total_count: number;
	    has_default: boolean;

	    static createFrom(source: any = {}) {
	        return new ModelPricingStorageStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.total_count = source["total_count"];
	        this.has_default = source["has_default"];
	    }
	}
	export class PortInfo {
	    preferred_port: number;
	    actual_port: number;
	    is_default: boolean;
	    was_occupied: boolean;

	    static createFrom(source: any = {}) {
	        return new PortInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preferred_port = source["preferred_port"];
	        this.actual_port = source["actual_port"];
	        this.is_default = source["is_default"];
	        this.was_occupied = source["was_occupied"];
	    }
	}
	export class RequestRecord {
	    id: string;
	    request_id: string;
	    timestamp: string;
	    client_ip: string;
	    user_agent: string;
	    method: string;
	    path: string;
	    channel: string;
	    endpoint: string;
	    endpoint_chain: string;
	    endpoint_chain_head: string;
	    endpoint_chain_tail: string;
	    endpoint_chain_length: number;
	    group: string;
	    model: string;
	    status: string;
	    http_status: number;
	    retry_count: number;
	    failure_reason?: string;
	    last_failure_reason?: string;
	    cancel_reason?: string;
	    privacy_redacted: boolean;
	    privacy_redaction_summary?: string;
	    input_tokens: number;
	    output_tokens: number;
	    cache_creation_tokens: number;
	    cache_creation_5m_tokens: number;
	    cache_creation_1h_tokens: number;
	    cache_read_tokens: number;
	    response_time: number;
	    is_streaming: boolean;
	    cost: number;
	    input_cost_usd: number;
	    output_cost_usd: number;
	    cache_creation_cost_usd: number;
	    cache_creation_5m_cost_usd: number;
	    cache_creation_1h_cost_usd: number;
	    cache_read_cost_usd: number;
	    billing_rule_snapshot: string;

	    static createFrom(source: any = {}) {
	        return new RequestRecord(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.request_id = source["request_id"];
	        this.timestamp = source["timestamp"];
	        this.client_ip = source["client_ip"];
	        this.user_agent = source["user_agent"];
	        this.method = source["method"];
	        this.path = source["path"];
	        this.channel = source["channel"];
	        this.endpoint = source["endpoint"];
	        this.endpoint_chain = source["endpoint_chain"];
	        this.endpoint_chain_head = source["endpoint_chain_head"];
	        this.endpoint_chain_tail = source["endpoint_chain_tail"];
	        this.endpoint_chain_length = source["endpoint_chain_length"];
	        this.group = source["group"];
	        this.model = source["model"];
	        this.status = source["status"];
	        this.http_status = source["http_status"];
	        this.retry_count = source["retry_count"];
	        this.failure_reason = source["failure_reason"];
	        this.last_failure_reason = source["last_failure_reason"];
	        this.cancel_reason = source["cancel_reason"];
	        this.privacy_redacted = source["privacy_redacted"];
	        this.privacy_redaction_summary = source["privacy_redaction_summary"];
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.cache_creation_tokens = source["cache_creation_tokens"];
	        this.cache_creation_5m_tokens = source["cache_creation_5m_tokens"];
	        this.cache_creation_1h_tokens = source["cache_creation_1h_tokens"];
	        this.cache_read_tokens = source["cache_read_tokens"];
	        this.response_time = source["response_time"];
	        this.is_streaming = source["is_streaming"];
	        this.cost = source["cost"];
	        this.input_cost_usd = source["input_cost_usd"];
	        this.output_cost_usd = source["output_cost_usd"];
	        this.cache_creation_cost_usd = source["cache_creation_cost_usd"];
	        this.cache_creation_5m_cost_usd = source["cache_creation_5m_cost_usd"];
	        this.cache_creation_1h_cost_usd = source["cache_creation_1h_cost_usd"];
	        this.cache_read_cost_usd = source["cache_read_cost_usd"];
	        this.billing_rule_snapshot = source["billing_rule_snapshot"];
	    }
	}
	export class RequestListResult {
	    requests: RequestRecord[];
	    total: number;
	    page: number;
	    page_size: number;

	    static createFrom(source: any = {}) {
	        return new RequestListResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requests = this.convertValues(source["requests"], RequestRecord);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.page_size = source["page_size"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RequestQueryParams {
	    page: number;
	    page_size: number;
	    start_date: string;
	    end_date: string;
	    status: string;
	    model: string;
	    channel: string;
	    endpoint: string;
	    group: string;

	    static createFrom(source: any = {}) {
	        return new RequestQueryParams(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.page = source["page"];
	        this.page_size = source["page_size"];
	        this.start_date = source["start_date"];
	        this.end_date = source["end_date"];
	        this.status = source["status"];
	        this.model = source["model"];
	        this.channel = source["channel"];
	        this.endpoint = source["endpoint"];
	        this.group = source["group"];
	        this.provider = source["provider"];
	    }
	}

	export class SaveFileResult {
	    success: boolean;
	    file_path: string;
	    file_name: string;
	    file_size: number;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new SaveFileResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.file_path = source["file_path"];
	        this.file_name = source["file_name"];
	        this.file_size = source["file_size"];
	        this.error = source["error"];
	    }
	}
export class SettingInfo {
	    id: number;
	    provider: string;
	    category: string;
	    key: string;
	    value: string;
	    value_type: string;
	    label: string;
	    description: string;
	    display_order: number;
	    enum_options?: store.EnumOption[];
	    requires_restart: boolean;
	    created_at: string;
	    updated_at: string;

	    static createFrom(source: any = {}) {
	        return new SettingInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.provider = source["provider"];
	        this.category = source["category"];
	        this.key = source["key"];
	        this.value = source["value"];
	        this.value_type = source["value_type"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.display_order = source["display_order"];
	        this.enum_options = this.convertValues(source["enum_options"], store.EnumOption);
	        this.requires_restart = source["requires_restart"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SettingsStorageStatus {
	    enabled: boolean;
	    total_count: number;
	    category_count: number;
	    is_initialized: boolean;

	    static createFrom(source: any = {}) {
	        return new SettingsStorageStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.total_count = source["total_count"];
	        this.category_count = source["category_count"];
	        this.is_initialized = source["is_initialized"];
	    }
	}
	export class SwitchKeyResult {
	    success: boolean;
	    message: string;
	    endpoint: string;
	    key_type: string;
	    new_index: number;
	    timestamp: string;

	    static createFrom(source: any = {}) {
	        return new SwitchKeyResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.message = source["message"];
	        this.endpoint = source["endpoint"];
	        this.key_type = source["key_type"];
	        this.new_index = source["new_index"];
	        this.timestamp = source["timestamp"];
	    }
	}
	export class SystemStatus {
	    version: string;
	    uptime: string;
	    uptime_seconds: number;
	    start_time: string;
	    proxy_port: number;
	    proxy_host: string;
	    proxy_running: boolean;
	    active_group: string;
	    config_path: string;
	    auth_enabled: boolean;

	    static createFrom(source: any = {}) {
	        return new SystemStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.uptime = source["uptime"];
	        this.uptime_seconds = source["uptime_seconds"];
	        this.start_time = source["start_time"];
	        this.proxy_port = source["proxy_port"];
	        this.proxy_host = source["proxy_host"];
	        this.proxy_running = source["proxy_running"];
	        this.active_group = source["active_group"];
	        this.config_path = source["config_path"];
	        this.auth_enabled = source["auth_enabled"];
	    }
	}
	export class TokenUsageData {
	    input_tokens: number;
	    output_tokens: number;
	    cache_creation_tokens: number;
	    cache_read_tokens: number;
	    total_tokens: number;

	    static createFrom(source: any = {}) {
	        return new TokenUsageData(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.cache_creation_tokens = source["cache_creation_tokens"];
	        this.cache_read_tokens = source["cache_read_tokens"];
	        this.total_tokens = source["total_tokens"];
	    }
	}
	export class TokenUsageHeatmapDay {
	    date: string;
	    bucket: string;
	    request_count: number;
	    input_tokens: number;
	    output_tokens: number;
	    cache_creation_tokens: number;
	    cache_read_tokens: number;
	    total_tokens: number;
	    intensity: number;

	    static createFrom(source: any = {}) {
	        return new TokenUsageHeatmapDay(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.bucket = source["bucket"];
	        this.request_count = source["request_count"];
	        this.input_tokens = source["input_tokens"];
	        this.output_tokens = source["output_tokens"];
	        this.cache_creation_tokens = source["cache_creation_tokens"];
	        this.cache_read_tokens = source["cache_read_tokens"];
	        this.total_tokens = source["total_tokens"];
	        this.intensity = source["intensity"];
	    }
	}
	export class UpdateChannelInput {
	    name: string;
	    website?: string;
	    priority: number;
	    model_mapping: Record<string, string>;

	    static createFrom(source: any = {}) {
	        return new UpdateChannelInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.website = source["website"];
	        this.priority = source["priority"];
	        this.model_mapping = source["model_mapping"];
	    }
	}

	export class UsageStatsData {
	    period: string;
	    total_requests: number;
	    success_rate: number;
	    avg_duration_ms: number;
	    total_cost_usd: number;
	    total_tokens: number;
	    failed_requests: number;

	    static createFrom(source: any = {}) {
	        return new UsageStatsData(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.period = source["period"];
	        this.total_requests = source["total_requests"];
	        this.success_rate = source["success_rate"];
	        this.avg_duration_ms = source["avg_duration_ms"];
	        this.total_cost_usd = source["total_cost_usd"];
	        this.total_tokens = source["total_tokens"];
	        this.failed_requests = source["failed_requests"];
	    }
	}
	export class UsageStatsQueryParams {
	    period: string;
	    start_date: string;
	    end_date: string;
	    status: string;
	    model: string;
	    channel: string;
	    endpoint: string;
	    group: string;

	    static createFrom(source: any = {}) {
	        return new UsageStatsQueryParams(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.period = source["period"];
	        this.start_date = source["start_date"];
	        this.end_date = source["end_date"];
	        this.status = source["status"];
	        this.model = source["model"];
	        this.channel = source["channel"];
	        this.endpoint = source["endpoint"];
	        this.group = source["group"];
	    }
	}
	export class UsageSummary {
	    total_requests: number;
	    all_time_total_requests: number;
	    today_requests: number;
	    success_requests: number;
	    failed_requests: number;
	    active_connections: number;
	    average_response_time: string;
	    total_tokens: number;
	    total_input_tokens: number;
	    total_output_tokens: number;
	    total_cost: number;
	    today_cost: number;
	    all_time_total_cost: number;
	    today_tokens: number;
	    all_time_total_tokens: number;

	    static createFrom(source: any = {}) {
	        return new UsageSummary(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_requests = source["total_requests"];
	        this.all_time_total_requests = source["all_time_total_requests"];
	        this.today_requests = source["today_requests"];
	        this.success_requests = source["success_requests"];
	        this.failed_requests = source["failed_requests"];
	        this.active_connections = source["active_connections"];
	        this.average_response_time = source["average_response_time"];
	        this.total_tokens = source["total_tokens"];
	        this.total_input_tokens = source["total_input_tokens"];
	        this.total_output_tokens = source["total_output_tokens"];
	        this.total_cost = source["total_cost"];
	        this.today_cost = source["today_cost"];
	        this.all_time_total_cost = source["all_time_total_cost"];
	        this.today_tokens = source["today_tokens"];
	        this.all_time_total_tokens = source["all_time_total_tokens"];
	    }
	}

}

export namespace store {

	export class EnumOption {
	    value: string;
	    label: string;
	    description: string;

	    static createFrom(source: any = {}) {
	        return new EnumOption(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.label = source["label"];
	        this.description = source["description"];
	    }
	}

}
