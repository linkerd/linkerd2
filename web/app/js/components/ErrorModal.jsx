import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { Icon, Modal } from 'antd';

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

  renderContainerErrors = errorsByContainer => {
    return _.map(errorsByContainer, (errors, container) => (
      <div key={`error-${container}`}>
        <p>Container: {container}</p>
        <p>Image: {_.get(errors, [0, "image"])}</p>
        <div className="error-text">
          {
            _.map(errors, (er, i) =>
              <code key={`error-msg-${i}`}>{er.message}</code>
            )
          }
        </div>
      </div>
    ));
  };

  renderPodErrors = podErrors => {
    let errorsByPodAndContainer = _(podErrors)
      .keys()
      .sortBy()
      .map(pod => {
        return {
          pod: pod,
          byContainer: _(podErrors[pod].errors)
            .groupBy( "container.container")
            .mapValues(v => _.map(v, "container"))
            .value()
        };
      }).value();

    return _.map(errorsByPodAndContainer, err => {
      return (
        <div className="conduit-pod-error" key={err.pod}>
          <h3>Pod: {err.pod}</h3>
          {this.renderContainerErrors(err.byContainer)}
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
