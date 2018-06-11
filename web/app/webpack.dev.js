/* global require, module, __dirname */

const merge = require('webpack-merge');
const baseWebpackConfig = require('./webpack.base');

module.exports = merge(baseWebpackConfig, {
  module: {
    rules: [
      {
        test: /\.jsx?$/,
        exclude: /node_modules/,
        use: [
          'babel-loader',
          {
            loader: 'eslint-loader',
            options: {
              fix: true,
              emitWarning: true
            }
          }
        ]
      }
    ]
  }
});
