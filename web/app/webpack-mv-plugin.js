// WebpackMvPlugin copies /dist/index_bundle.js.out to /dist/index_bundle.js
class WebpackMvPlugin {
  apply(compiler) {
    compiler.hooks.compilation.tap('webpack-mv-plugin', function(compilation) {
      compilation.hooks.htmlWebpackPluginAfterHtmlProcessing.tapAsync(
        'webpack-mv-plugin',
        function(htmlPluginData, cb) {
          var out = htmlPluginData.plugin.childCompilationOutputName;
          htmlPluginData.plugin.childCompilationOutputName = out.substring(0, out.indexOf('.out'));
          cb(null, htmlPluginData);
        }
      );
    });
  };
}

module.exports = WebpackMvPlugin;
