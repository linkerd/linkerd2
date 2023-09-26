const { formatter } = require("@lingui/format-json");

module.exports = {
    locales: ["en", "es"],
    catalogs: [
        {
            path: "js/locales/{locale}",
            include: ["js"],
            format: formatter({ style: "minimal" }),
        },
    ],
};
