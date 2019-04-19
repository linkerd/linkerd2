exports.config = {
  port: 9515, // default for ChromeDriver
  path: '/',
  services: ['chromedriver'],
  runner: 'local',
  specs: [
      './integration/specs/**/*.js'
  ],
    suites: {
    sidebar: [
        './integration/specs/sidebar/*.js'
    ],
    daemonset: [
        './integration/specs/daemonset/*.js'
    ]
},
  exclude: [
      // 'path/to/excluded/files'
  ],
  maxInstances: 10,
  capabilities: [{browserName: 'chrome'}],
  bail: 0,
  baseUrl: 'http://localhost',
  waitforTimeout: 1000000,
  connectionRetryTimeout: 90000000,
  connectionRetryCount: 3,
  framework: 'mocha',
  mochaOpts: {
      ui: 'bdd',
      timeout: 6000000
  }
}
