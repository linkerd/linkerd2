exports.config = {
  runner: 'local',
  user: process.env.SAUCE_USERNAME,
  key: process.env.SAUCE_ACCESS_KEY,
  service: ['sauce'],
  sauceConnect: true,
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
  // Patterns to exclude.
  exclude: [
      // 'path/to/excluded/files'
  ],
  maxInstances: 10,
  capabilities: [
    {browserName: 'firefox', platform: 'Windows 10', version: '60.0'},
    {browserName: 'chrome', platform: 'OS X 10.13', version: '69.0'}
  ],
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
