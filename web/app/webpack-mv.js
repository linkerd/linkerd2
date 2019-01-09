// WebpackMv is a plugin to copy /dist/index_bundle.js.out to /dist/index_bundle.js
class WebpackMv {
  apply(compiler) {
    compiler.hooks.compilation.tap('webpack-mv', function(compilation) {
      // check for undefined, in case SpeedMeasurePlugin is enabled:
      // https://github.com/stephencookdev/speed-measure-webpack-plugin/issues/44
      if (compilation.hooks.htmlWebpackPluginAfterHtmlProcessing !== undefined) {
        compilation.hooks.htmlWebpackPluginAfterHtmlProcessing.tapAsync(
          'webpack-mv',
          function(htmlPluginData, cb) {
            var out = htmlPluginData.plugin.childCompilationOutputName;
            htmlPluginData.plugin.childCompilationOutputName = out.substring(0, out.indexOf('.out'));
            cb(null, htmlPluginData);
          }
        );
      }
    });
  };
}

module.exports = WebpackMv;
