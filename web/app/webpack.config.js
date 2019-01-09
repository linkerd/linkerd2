/* global require, module, __dirname */

const path = require('path');
const CleanWebpackPlugin = require('clean-webpack-plugin');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const LodashModuleReplacementPlugin = require('lodash-webpack-plugin');
const WebpackMv = require('./webpack-mv.js');

// analyze plugin speeds
const SpeedMeasurePlugin = require("speed-measure-webpack-plugin");
// disable by default until this is fixed:
// https://github.com/stephencookdev/speed-measure-webpack-plugin/issues/44
const smp = new SpeedMeasurePlugin({disable: true});

// uncomment here and in plugins to analyze webpack bundle size
// const BundleAnalyzerPlugin = require('webpack-bundle-analyzer').BundleAnalyzerPlugin;

module.exports = smp.wrap({
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
    new HtmlWebpackPlugin({
      inject: false,
      filename: "index_bundle.js.out",
      template: 'lodash/index_bundle.js.lodash.tmpl',
    }),
    new LodashModuleReplacementPlugin({
      // 'chain': true,
      'collections': true,
      'paths': true
    }),
    new WebpackMv()
  ]
});
