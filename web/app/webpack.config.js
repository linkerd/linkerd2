/* global require, module, __dirname */

module.exports = process.env.NODE_ENV === 'production'
  ? require('./webpack.prod.js')
  : require('./webpack.dev.js')
