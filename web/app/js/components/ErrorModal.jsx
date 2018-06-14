import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { Icon, Modal } from 'antd';

const ContainerError = (container, errors) => {
  return (
    <div key={`error-${container}`}>
      <p>Container: {_.get(errors, [0, "container", "container"])}</p>
      <p>Image: {_.get(errors, [0, "container", "image"])}</p>
      <div className="error-text">
        {
        _.map(errors, (er, i) => (
          <code key={`error-msg-${i}`}>
            {_.get(er, ["container", "message"])}
          </code>
        ))
      }
      </div>
    </div>
  );
};

export default class ErrorModal extends React.Component {
  static propTypes = {
    errors: PropTypes.shape({}).isRequired,
    resourceName: PropTypes.string.isRequired,
    resourceType: PropTypes.string.isRequired
  }

  showModal = () => {
    Modal.error({
      title: `Errors in ${this.props.resourceType} ${this.props.resourceName}`,
      width: 800,
      maskClosable: true,
      content: this.renderPodErrors(this.props.errors),
      onOk() {},
    });
  }

  renderPodErrors = podErrors => {
    let sortedPods = _.sortBy(_.keys(podErrors));
    return _.map(sortedPods, pod => {
      let errorsByContainer = _.groupBy(podErrors[pod].errors, "container.container");

      return (
        <div className="conduit-pod-error" key={pod}>
          <h3>Pod: {pod}</h3>
          {_.map(errorsByContainer, (errors, container) => ContainerError(container, errors))}
        </div>
      );
    });
  }

  render() {
    return (
      <Icon type="warning" className="conduit-error-icon" onClick={this.showModal} />
    );
  }
}
