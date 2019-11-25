import PropTypes from 'prop-types';
import React from 'react';
import { friendlyTitle } from './util/Utils.js';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import { withTranslation } from 'react-i18next';

class AddResources extends React.Component {
  static propTypes = {
    resourceName: PropTypes.string.isRequired,
    resourceType: PropTypes.string.isRequired,
    t: PropTypes.func.isRequired,
  }

  render() {
    const {resourceName, resourceType, t} = this.props;

    return (
      <div className="mesh-completion-message">
        {t("message4", { type: friendlyTitle(resourceType).singular, name: resourceName })}
        {incompleteMeshMessage()}
      </div>
    );
  }
}

export default withTranslation(["serviceMesh"])(AddResources);
