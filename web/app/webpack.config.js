/* global require, module, __dirname */

const path = require('path');
const { CleanWebpackPlugin } = require('clean-webpack-plugin');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const LodashModuleReplacementPlugin = require('lodash-webpack-plugin');
const ESLintPlugin = require('eslint-webpack-plugin');

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
        use: ['babel-loader']
      },
      {
        test: /\.css$/,
        use: [
          'style-loader',
          { loader: 'css-loader', options: { importLoaders: 1 } },
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
    new ESLintPlugin({
      extensions: ['.jsx', '.js'],
      fix: process.env.NODE_ENV === 'development',
      emitWarning: process.env.NODE_ENV === 'development',
      files: ['js', 'test']
    }),
    new CleanWebpackPlugin({
      protectWebpackAssets: false
    }),
    new LodashModuleReplacementPlugin({
      // 'chain': true,
      'collections': true,
      'paths': true,
      'shorthands': true
    }),
    // compile the bundle with hashed filename into index_bundle.js
    new HtmlWebpackPlugin({
      cache: false,
      inject: false,
      filename: 'index_bundle.js',
      template: 'index_bundle.js.lodash.tmpl',
      // this is important! Production builds minimize the template, causing
      // the comment on the first line to be for the entire file and then
      // nothing loads.
      minify: false
    })
  ],
};
