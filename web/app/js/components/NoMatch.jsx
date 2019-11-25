import PropTypes from 'prop-types';
import React from 'react';
import { withTranslation } from 'react-i18next';

class NoMatch extends React.Component {
  render() {
    const { t } = this.props;
    return (
      <div>
        <h3>404</h3>
        <div>{t("Page not found.")}</div>
      </div>
    );
  }
}

NoMatch.propTypes = {
  t: PropTypes.func.isRequired,
};

export default withTranslation()(NoMatch);
