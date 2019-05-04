exports.config = {
  port: 9515, // default for ChromeDriver
  path: '/',
  services: ['chromedriver'],
  runner: 'local',
  specs: [
    './integration/specs/**/*.js'
  ],
  suites: {
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
  waitforTimeout: 180000,
  connectionRetryTimeout: 300000,
  connectionRetryCount: 3,
  framework: 'mocha',
  mochaOpts: {
    ui: 'bdd',
    timeout: 900000
  }
};