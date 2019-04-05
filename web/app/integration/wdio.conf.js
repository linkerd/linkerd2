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
    overview: [
        './integration/specs/overview/*.js'
    ],
    resources: [
         './integration/specs/resources/*.js'
    ],
    deployments: [
        './integration/specs/deployments/*.js'
    ],
    authorities: [
        './integration/specs/authorities/*.js'
    ],
    namespaces: [
        './integration/specs/namespaces/*.js'
    ],
    daemonsets: [
        './integration/specs/daemonsets/*.js'
    ],
    pods: [
        './integration/specs/pods/*.js'
    ],
    replicationcontorller: [
        './integration/specs/replication_cont/*.js'
    ],
    statefulsets: [
        './integration/specs/statefulsets/*.js'
    ],
    servicemesh: [
        './integration/specs/service_mesh/*.js'
    ],
},
  exclude: [
      // 'path/to/excluded/files'
  ],
  maxInstances: 10,
  capabilities: [{browserName: 'chrome', }],
  bail: 0,
  baseUrl: 'http://localhost',
  waitforTimeout: 10000,
  connectionRetryTimeout: 90000,
  connectionRetryCount: 3,
  framework: 'mocha',
  mochaOpts: {
      ui: 'bdd',
      timeout: 60000
  }
}
