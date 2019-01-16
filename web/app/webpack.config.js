/* global require, module, __dirname */

const path = require('path');
const CleanWebpackPlugin = require('clean-webpack-plugin');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const LodashModuleReplacementPlugin = require('lodash-webpack-plugin');
const WebpackMvPlugin = require('./webpack-mv-plugin.js');

// uncomment here and in plugins to analyze webpack bundle size
// const BundleAnalyzerPlugin = require('webpack-bundle-analyzer').BundleAnalyzerPlugin;

module.exports = {
  mode: process.env.NODE_ENV === 'production' ? 'production' : 'development',
  entry: './js/index.js',
  devServer: {
    writeToDisk: true
  },
  output: {
    path: path.resolve(__dirname, 'dist'),
    publicPath: 'dist/',
    filename: '[name].[contenthash].js'
  },
  devtool: 'cheap-module-source-map',
  externals: {
    cheerio: 'window',
    'react/addons': 'react',
    'react/lib/ExecutionEnvironment': 'react',
    'react/lib/ReactContext': 'react',
    'react-addons-test-utils': 'react-dom',
  },
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
              emitWarning: process.env.NODE_ENV === 'development'
            }
          }
        ]
      },
      {
        test: /\.css$/,
        use: [
          'style-loader',
          { loader: 'css-loader', options: { importLoaders: 1, minimize: true } },
        ]
      },
      {
        test: /\.(png|jpg|gif|eot|svg|ttf|woff|woff2)$/,
        use: [
          {
            loader: 'file-loader',
            options: {
              name: 'img/[name].[ext]'
            }
          }
        ]
      }
    ]
  },
  plugins: [
    // new BundleAnalyzerPlugin(), // uncomment to analyze bundle size
    new CleanWebpackPlugin(['dist']),
    new LodashModuleReplacementPlugin({
      // 'chain': true,
      'collections': true,
      'paths': true
    }),
    // compile the bundle with hashed filename into index_bundle.js.out
    new HtmlWebpackPlugin({
      inject: false,
      filename: "index_bundle.js.out",
      template: 'index_bundle.js.lodash.tmpl',
    }),
    // move /dist/index_bundle.js.out to /dist/index_bundle.js
    new WebpackMvPlugin()
  ]
};
