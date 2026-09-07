export namespace agent {
	
	export class Attachment {
	    name: string;
	    mime: string;
	    dataUrl: string;
	    text: string;
	    isImage: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Attachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.mime = source["mime"];
	        this.dataUrl = source["dataUrl"];
	        this.text = source["text"];
	        this.isImage = source["isImage"];
	    }
	}

}

export namespace chatstore {
	
	export class Session {
	    id: string;
	    title: string;
	    itemsJson: string;
	    history: llm.Message[];
	    // Go type: time
	    updatedAt: any;
	    costUsd?: number;
	    inputTokens?: number;
	    outputTokens?: number;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.itemsJson = source["itemsJson"];
	        this.history = this.convertValues(source["history"], llm.Message);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.costUsd = source["costUsd"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
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
	export class ProjectBundle {
	    project: string;
	    activeId: string;
	    sessions: Session[];
	
	    static createFrom(source: any = {}) {
	        return new ProjectBundle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project = source["project"];
	        this.activeId = source["activeId"];
	        this.sessions = this.convertValues(source["sessions"], Session);
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

}

export namespace llm {
	
	export class ImageURL {
	    url: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageURL(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.detail = source["detail"];
	    }
	}
	export class ContentPart {
	    type: string;
	    text?: string;
	    image_url?: ImageURL;
	
	    static createFrom(source: any = {}) {
	        return new ContentPart(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.text = source["text"];
	        this.image_url = this.convertValues(source["image_url"], ImageURL);
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
	export class FunctionCall {
	    name: string;
	    arguments: string;
	
	    static createFrom(source: any = {}) {
	        return new FunctionCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.arguments = source["arguments"];
	    }
	}
	
	export class ToolCall {
	    id: string;
	    type: string;
	    function: FunctionCall;
	
	    static createFrom(source: any = {}) {
	        return new ToolCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.function = this.convertValues(source["function"], FunctionCall);
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
	export class Message {
	    role: string;
	    content?: string;
	    name?: string;
	    tool_call_id?: string;
	    tool_calls?: ToolCall[];
	    reasoning_content?: string;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	        this.name = source["name"];
	        this.tool_call_id = source["tool_call_id"];
	        this.tool_calls = this.convertValues(source["tool_calls"], ToolCall);
	        this.reasoning_content = source["reasoning_content"];
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

}

export namespace main {
	
	export class UsageStats {
	    costUsd: number;
	    inputTokens: number;
	    outputTokens: number;
	    chatCostUsd: number;
	    chatInputTokens: number;
	    chatOutputTokens: number;
	
	    static createFrom(source: any = {}) {
	        return new UsageStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.costUsd = source["costUsd"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.chatCostUsd = source["chatCostUsd"];
	        this.chatInputTokens = source["chatInputTokens"];
	        this.chatOutputTokens = source["chatOutputTokens"];
	    }
	}

}

export namespace rules {
	
	export class Rule {
	    source: string;
	    path: string;
	    name: string;
	    description: string;
	    alwaysApply: boolean;
	    globs: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new Rule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.path = source["path"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.alwaysApply = source["alwaysApply"];
	        this.globs = source["globs"];
	        this.content = source["content"];
	    }
	}
	export class Bundle {
	    globalDir: string;
	    projectDir: string;
	    global: Rule[];
	    project: Rule[];
	
	    static createFrom(source: any = {}) {
	        return new Bundle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.globalDir = source["globalDir"];
	        this.projectDir = source["projectDir"];
	        this.global = this.convertValues(source["global"], Rule);
	        this.project = this.convertValues(source["project"], Rule);
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

}

export namespace workspace {
	
	export class Entry {
	    name: string;
	    path: string;
	    isDir: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	    }
	}
	export class Project {
	    name: string;
	    path: string;
	    opened: string;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.opened = source["opened"];
	    }
	}

}

export namespace zai {
	
	export class AccountBalance {
	    ok: boolean;
	    availableUsd: number;
	    usedUsd: number;
	    source: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountBalance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.availableUsd = source["availableUsd"];
	        this.usedUsd = source["usedUsd"];
	        this.source = source["source"];
	        this.detail = source["detail"];
	    }
	}

}

