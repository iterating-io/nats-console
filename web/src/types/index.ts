export type Operator = {
    name: string;
};

export type User = {
    name: string;
    publicKey: string;
    publishAllow: string[];
    publishDeny?: string[];
};

export type Account = {
    name: string;
    operator: string;
    publicKey: string;
    publishAllow: string[];
    subscribeAllow: string[];
    users: User[];
    jsEnabled: boolean;
    sourceEnabled?: boolean;
    isSystem?: boolean;
};
