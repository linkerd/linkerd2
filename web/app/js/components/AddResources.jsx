import _ from 'lodash';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import PropTypes from 'prop-types';
import React from 'react';

export default class AddResources extends React.Component {
  static propTypes = {
    resourceName: PropTypes.string.isRequired,
    resourceType: PropTypes.string.isRequired
  }

  render() {
    const {resourceName, resourceType} = this.props;

    return (
      <div className="mesh-completion-message">
        {_.startCase(resourceType)} {resourceName} is not in the mesh.
        {incompleteMeshMessage()}
      </div>
    );
  }
}
