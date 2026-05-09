type AccountPermissions = {
    publishAllow: string[];
    subscribeAllow: string[];
};

export const JS_CONSUMER_API_PUBLISH = [
    "$JS.API.INFO",
    "$JS.API.STREAM.INFO.>",
    "$JS.API.STREAM.NAMES",
    "$JS.API.STREAM.LIST",
    "$JS.API.STREAM.CREATE.>",
    "$JS.API.STREAM.UPDATE.>",
    "$JS.API.STREAM.DELETE.>",
    "$JS.API.CONSUMER.INFO.>",
    "$JS.API.CONSUMER.NAMES.>",
    "$JS.API.CONSUMER.LIST.>",
    "$JS.API.CONSUMER.MSG.NEXT.>",
    "$JS.API.CONSUMER.CREATE.>",
    "$JS.API.CONSUMER.DELETE.>",
    "$JS.ACK.>",
];

export const JS_CONSUMER_API_SUBSCRIBE = ["_INBOX.>"];

export const isJSConsumerApiPublishSubject = (subject: string): boolean =>
    JS_CONSUMER_API_PUBLISH.includes(subject);

export const isJSConsumerApiSubscribeSubject = (subject: string): boolean =>
    JS_CONSUMER_API_SUBSCRIBE.includes(subject);

export const accountHasJSConsumerApiAccess = (
    account: AccountPermissions,
): boolean =>
    JS_CONSUMER_API_PUBLISH.every((subject) =>
        account.publishAllow.includes(subject),
    ) &&
    JS_CONSUMER_API_SUBSCRIBE.every((subject) =>
        account.subscribeAllow.includes(subject),
    );
